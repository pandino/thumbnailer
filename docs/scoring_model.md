# How the thumbnailer's scoring model works

This document explains how the thumbnailer predicts, for each movie, whether you will
end up **deleting** it or **archiving** it. It starts from the basic ideas and assumes no
machine-learning background. It then goes into the technical details of what the code does.

---

## Key Terms at a Glance

* **Embedding:** A list of numbers (here 768) representing an image's content. Similar images get similar vectors.
* **Logit:** The raw weighted sum calculated by the model before converting to a probability.
* **Logistic Regression:** A linear model that turns a weighted sum of features into a probability via the sigmoid function.
* **Calibration (Platt Scaling):** Adjusting raw output probabilities so predicted percentages reflect true historical decision frequencies.
* **AUC (Area Under the Curve):** Evaluates ranking quality—the chance that a random positive example scores higher than a random negative one.
* **Brier Score:** Mean squared error between predicted probabilities and actual outcomes (0 = perfect, lower is better).

---

## Table of contents

1. [The problem in one paragraph](#1-the-problem-in-one-paragraph)
2. [The big picture: architecture, worker, and execution](#2-the-big-picture-architecture-worker-and-execution)
3. [Where the training data comes from: your decisions](#3-where-the-training-data-comes-from-your-decisions)
4. [Turning pictures into numbers: embeddings](#4-turning-pictures-into-numbers-embeddings)
5. [From a grid to one vector per movie](#5-from-a-grid-to-one-vector-per-movie)
6. [Learning your taste: logistic regression](#6-learning-your-taste-logistic-regression)
7. [Three outcomes, two models: the ordinal trick](#7-three-outcomes-two-models-the-ordinal-trick)
8. [Making the percentages honest: calibration](#8-making-the-percentages-honest-calibration)
9. [Weights: not every decision means the same](#9-weights-not-every-decision-means-the-same)
10. [How good is it? AUC, Brier score and the live check](#10-how-good-is-it-auc-brier-score-and-the-live-check)
11. [How the scores are used in the slideshow](#11-how-the-scores-are-used-in-the-slideshow)
12. [Limits and what could improve it](#12-limits-and-what-could-improve-it)
13. [Glossary](#13-glossary)

---

## 1. The problem in one paragraph

Every movie that arrives in the library gets a **thumbnail grid**: 32 frames taken at
regular intervals, arranged as 8 columns × 4 rows. You look at these grids in the slideshow
and make a first decision. **Delete** means "not my genre". **Space/→ (pass)** means "looks
interesting, I want to watch it". **Archive** means "keep it forever". Movies you pass are
later watched in MPV, where you make a final decision: delete or archive. The scoring model
looks at the grid of a movie **you haven't judged yet** and estimates two chances: that you
will end up deleting it, and that you will end up archiving it. It learns this only from the
decisions you have already made. Nobody tells it what you like.

---

## 2. The big picture: architecture, worker, and execution

### System Architecture

The system is split across machines because the workload varies significantly:

| Part | Where it runs | What it does |
|---|---|---|
| **Thumbnailer server** | `ichinose` (weak CPU, no GPU) | Stores grids, your decisions, and scores; serves the slideshow. |
| **Worker (`scripts/taste/taste.py`)** | `shun` (always-on Celeron N5105), as a podman container | Executes computations: embeds grids, trains models, scores movies. |
| **User** | Slideshow + MPV | Generates decisions that serve as new training data. |

### The Execution Loop

1. A new movie arrives, and the server generates its thumbnail grid.
2. The worker, running in the container `container-thumbnailer-taste` on `shun` and waking every 15 minutes (`--every 15`), identifies un-embedded grids.
3. The worker downloads new grids, extracts 16 representative frame vectors, averages them into an embedding vector (§4–5), and uploads the result.
4. The worker fetches **all** stored embeddings and decision logs, retrains both classification models from scratch, and computes scores for all undecided items.
5. Uploaded scores populate badges and sorting options in the slideshow UI.
6. New user interactions are logged, serving as training data for subsequent worker passes.

### Hardware, Lock Files, and Timings

Execution is handled by `scripts/taste/taste.py` using internal server endpoints. A lock file prevents concurrent runs, while time-boxing (`--max-minutes`, default 20) ensures large backlogs are processed across multiple runs.

- **Embedding performance (16 frames per grid):**
  - Desktop GPU (`RX 6700 XT`, `--device cuda`, `HSA_OVERRIDE_GFX_VERSION=10.3.0`): **~0.16s / grid**
  - Desktop CPU (`Ryzen 7 5700X3D`): **~1.3s / grid**
  - Low-power node (`shun`, Celeron N5105, 4 cores, no AVX): **~14s / grid**
- **Retraining & Scoring:** Retraining both models and scoring all ~2,170 movies takes a few seconds to ~40 seconds, depending on the machine. With ~9 new movies a day, `shun` spends a few minutes a day on this. The desktop GPU setup remains available for one-off jobs, such as re-embedding everything after a model change. Embeddings across desktop and CPU nodes produce identical cosine similarity vectors (`1.0000`).

---

## 3. Where the training data comes from: your decisions

Machine learning needs **examples with answers**, called *labels*. Here the examples are
movies and the answers are what you did with them.

### The decision log

Every decision is written to an append-only `decisions` table on the server. Each row holds
the movie, the **stage** (`slideshow` or `player`), the **action** (`delete`, `pass`,
`archive` or `undo`) and the time. Nothing is ever erased. When the server later deletes or
archives the movie file, the grid is moved to `thumbnails/decided/` instead of being
deleted. This means the training history survives the movie itself.

### From log to outcome

The worker reduces the log to one **final outcome** per movie (`final_decisions()`):

- **Undo** cancels the previous decision on the same movie and stage, as if it never
  happened.
- A standing **delete** or **archive**, from either stage, is final. If there are both (say,
  archived in the slideshow, deleted later after watching), the later one wins.
- A standing **pass** with nothing after it means **undecided**: you want to watch the movie
  before deciding.
- A **skip** (`S`) records nothing, so it teaches nothing.

The outcomes are ordered: **deleted < undecided < archived**. This order is important. A
passed movie isn't "neutral", it sits *between* the two extremes: you liked the grid more
than the ones you deleted, but you haven't committed to keeping it.

Today the training set holds roughly 246 deleted, 1,215 undecided and 495 archived movies.
About 140 deletions and the archived movies were reconstructed once, at the start
("backfill"). They come from a database snapshot and from generating grids for the movies
already in the archive folder.

---

## 4. Turning pictures into numbers: embeddings

### The idea

A computer can't compare two pictures by "meaning" directly. Pixel-by-pixel comparison is
useless: two scenes of the same kind can share almost no identical pixels. The trick is to
use a **pre-trained image model** that turns any picture into a list of numbers, a
**vector**, such that *pictures with similar content get similar vectors*. This list is
called an **embedding**. You can think of it as coordinates on a huge map. Each picture gets
a position, and pictures about similar things land close together.

The model used here is **SigLIP 2** (`ViT-B-16-SigLIP2-256`, weights `webli`), loaded
through the `open_clip` library. Google trained it on billions of image–caption pairs from
the web. It learned to place an image and a sentence describing it close together. As a side
effect, its image vectors capture a lot of semantics: setting, number of people, lighting,
style, clothing, studio vs amateur look, and so on. Nobody trained it on *your* taste; it
just provides a good "map". Your taste is learned on top of it (§6).

### The technicalities

- **ViT-B/16** means a *Vision Transformer*, "Base" size (about 86 million parameters in the
  image part). It cuts the input image into 16×16-pixel patches and processes them like words
  in a sentence.
- **256** is the input resolution: each frame is resized to 256×256. The grid tiles are
  320×180, so 256 px is close to their native size. The 384 px variant would only upscale
  them and is about 2.5× slower on a CPU.
- The output is a vector of **768 numbers**. The worker **L2-normalises** it, which means it
  scales the vector to length 1. Only its *direction* matters then, and the similarity of two
  frames is the cosine of the angle between their vectors.
- Embeddings are stored on the server (`embeddings` table) with a model key such as
  `ViT-B-16-SigLIP2-256/webli/f16`. A grid is embedded **once**. Retraining only reads the
  stored vectors, which is why it is fast.

---

## 5. From a grid to one vector per movie

A movie has one grid, but the grid holds 32 separate frames. Embedding the whole grid as one
picture works badly: the model would shrink a 2,600×740 sheet to 256×256, and every frame
would turn into a smudge. The worker therefore works frame by frame (`frames_of()`):

1. **Cut the grid into tiles.** The server records each grid's layout (columns, rows, tile
   size, padding), so the worker computes the exact crop box of every tile (`tile_boxes()`).
   These are exactly the pixels ffmpeg sampled from the movie. No frames need to be stored
   separately.
2. **Drop blank tiles.** Short movies leave empty tiles, and some frames are black. A tile
   whose grey levels barely vary (standard deviation below 6) is discarded.
3. **Keep 16 evenly spaced frames** out of what's left. This halves the work with almost no
   loss: neighbouring frames are often similar.
4. **Embed each frame**, normalise each vector, **average** them, and normalise the average
   again. The result is one 768-number vector that summarises the whole movie: its "centre
   of gravity" on the map.

Grids with an unknown layout (a few very old, imported ones) are skipped by default, because
the crop boxes can't be trusted.

---

## 6. Learning your taste: logistic regression

### The idea

Now every movie is a point on the 768-dimensional map, and some of the points carry labels
("deleted", "archived"…). The question becomes: *is there a direction on the map along which
deleted movies sit at one end and kept movies at the other?* If so, a new movie's position
along that direction predicts what you'll do with it.

**Logistic regression** finds exactly that. It learns one **weight** ($w_i$) per feature in the vector, plus an overall bias ($b$). For a movie vector $\mathbf{x} = (x_1, \dots, x_{768})$, it computes a score (logit) and passes it through the sigmoid function $\sigma(z)$ to get a predicted probability:

$$\text{logit} = b + \sum_{i=1}^{768} w_i x_i$$

$$P(\text{positive}) = \sigma(\text{logit}) = \frac{1}{1 + e^{-\text{logit}}}$$

Training selects the weights that align predicted probabilities with past decisions as closely as possible.

Why such a simple model? The hard part, understanding picture content, is performed by SigLIP. On top of a high-quality embedding, a linear classifier is sufficient, trains in seconds, avoids over-fitting on ~2,000 examples, and yields consistent run-to-run output.

### The technicalities

- **Regularisation ($C$).** With 768 weights and ~2,000 examples, regularisation penalises excessively large weight values to avoid memorising noise. Small $C$ enforces a smoother model, while larger $C$ fits training data closer. The worker evaluates 9 values from 0.01 to 100 (`LogisticRegressionCV`) using cross-validation; currently $C \approx 31.6$ yields optimal performance.
- **Class balance.** Deleted movies represent ~13% of historical data. To prevent trivial majority-class prediction, `class_weight="balanced"` reweights loss penalties inversely proportional to class frequencies during training. The resulting probability shifts are corrected during calibration (§8).

---

## 7. Three outcomes, two models: the ordinal trick

Logistic regression handles binary classifications, whereas outcomes are 3-tiered. The system employs an ordinal classification scheme (Frank & Hall) by training **two separate binary classifiers** over all data points:

| Model | Classification Question | Positive ($y=1$) | Negative ($y=0$) |
|---|---|---|---|
| **Delete Model** | "Will this movie end up deleted?" | Deleted | Undecided + Archived |
| **Archive Model** | "Will this movie end up archived?" | Archived | Deleted + Undecided |

### Probability Allocation & Consistency

Each classifier predicts an independent probability ($p_{\text{delete}}$ and $p_{\text{archive}}$). The remaining probability mass represents the likelihood of remaining undecided ($p_{\text{undecided}}$):

$$p_{\text{undecided}} = 1 - p_{\text{delete}} - p_{\text{archive}}$$

Because the models train independently, raw predictions can occasionally sum to $> 1.0$. The function `consistent()` enforces proper probability bounds by scaling both values proportionally whenever $p_{\text{delete}} + p_{\text{archive}} > 1$:

$$p_{\text{delete}} \leftarrow \frac{p_{\text{delete}}}{p_{\text{delete}} + p_{\text{archive}}}, \quad p_{\text{archive}} \leftarrow \frac{p_{\text{archive}}}{p_{\text{delete}} + p_{\text{archive}}}$$

Evaluating this 2-model ordinal approach against a single "delete vs. keep" baseline showed superior separation across all outcomes (AUC: Deleted vs Archived **0.934**, Deleted vs Passed **0.775**, Archived vs Passed **0.822**).

---

## 8. Making the percentages honest: calibration

### The problem

Class balancing (§6) adjusts sample weights to optimize ranking performance, but skews output probabilities toward higher values. Uncalibrated "60% delete" model predictions corresponded to an actual deletion frequency of roughly 15%.

### The fix: Platt scaling

To restore accurate probabilities, the system applies **Platt scaling**—a secondary single-variable logistic regression mapping raw model logits to real observed frequencies:

1. **Out-of-Fold Generation:** 5-fold cross-validation generates unbiased, out-of-fold logit predictions across the training set.
2. **Calibration Fitting:** The Platt scalar model is fitted on these out-of-fold predictions against actual binary outcomes.

Because Platt scaling is monotonic, ranking orders remain identical while probability badges accurately represent historical likelihoods. Applying calibration reduced the Delete model's Brier score from 0.169 to 0.079.

---

## 9. Weights: not every decision means the same

Training examples are weighted according to signal quality relative to visual thumbnail evaluation:

- **Slideshow Deletions:** Directly evaluated on visual grid content $\rightarrow$ Weight = **1.0**.
- **Player (MPV) Deletions:** Rejected after watching $\rightarrow$ Lower visual correlation; Weight = **0.5** in Delete model (`--watched-delete-weight`). Retains full weight (**1.0**) as non-archived in Archive model.
- **Archive Decisions:** Represents explicit long-term retention $\rightarrow$ Weight = **1.0**.

Weights apply across model fitting, hyperparameter selection ($C$), and Platt calibration.

---

## 10. How good is it? AUC, Brier score and the live check

### Cross-validation

Metrics are evaluated using 5-fold cross-validation to prevent training set leakage.

### Ranking Accuracy (AUC)

- **Delete Model (~0.88 AUC):** Strongly separates genre rejections based on thumbnail grid features.
- **Archive Model (~0.75 AUC):** Evaluates overall post-watching preference, which depends on factors beyond static image frames (e.g., plot pacing, audio, acting quality).

### Probability Accuracy (Brier Score)

- **Delete Model:** **~0.06** (vs. baseline uncalibrated ~0.11).
- **Archive Model:** **~0.12**.

### Live Decisions Check

The system logs the exact $p_{\text{delete}}$ shown on screen whenever a decision is made. A rolling window of up to 300 live slideshow decisions evaluates actual performance against displayed probabilities. 

Currently, live AUC sits at **~0.75**. This is lower than the cross-validated 0.88, primarily because older decisions in the evaluation window were scored by earlier model iterations trained on smaller datasets. Historical predictions can be filtered by model version (`model_version`) on the control page to evaluate current model performance independently.

---

## 11. How the scores are used in the slideshow

- **Badge Display:** Displays the likeliest outcome capped at 99% (e.g., "Delete 72%" or "Archive 26%") to prevent false certainty.
- **Sorting Modes:**
  - *Likely delete first:* Sorts by descending $p_{\text{delete}}$.
  - *Likely archive first:* Sorts by descending $p_{\text{archive}}$.
  - *Uncertain:* Sorts items where $p_{\text{undecided}}$ is highest to refine model boundary decisions.
  - *Random (Default):* Standard slideshow order.
- **Unscored Items:** Unprocessed arrivals sit at the end of sorted queues until evaluated on the next worker pass.

---

## 12. Limits and what could improve it

- **Visual-only modality:** Narrative pacing, audio, and performance quality are unobserved, placing a ceiling on archive prediction accuracy.
- **Duration bias:** Archived media correlates with shorter durations (median 37 min vs. 62 min for deleted). Duration-stratified evaluations show AUCs of 0.89–0.99, confirming predictions extend beyond runtime length.
- **Selection bias:** Heavy usage of "Likely delete first" biases incoming training samples toward predicted deletions; using "Uncertain" or "Random" modes mitigates this shift.
- **Potential improvements:**
  1. Incorporate duration and video resolution as explicit model features.
  2. Increase sampled grid frame evaluation count beyond 16 frames.
  3. Fine-tune image encoder backbone weights once sample sizes increase significantly.

---

## 13. Glossary

| Term | Meaning |
|---|---|
| **AUC** | Area Under the ROC Curve; measures probability that a positive item ranks higher than a negative one. |
| **Brier Score** | Mean squared difference between predicted probabilities and actual outcomes. Lower is better. |
| **Calibration** | Mapping model output values so predicted percentages match observed outcome frequencies. |
| **Class Balancing** | Adjusting class weighting penalties to prevent model bias toward majority classes. |
| **Cross-Validation** | Evaluating model performance iteratively on out-of-fold partitions. |
| **Embedding** | A 768-dimensional vector encoding high-level semantic frame features. |
| **L2 Normalisation** | Scaling vectors to unit length $\|v\| = 1$ so comparisons rely purely on cosine direction. |
| **Logistic Regression** | Linear classifier computing class probabilities using the sigmoid function. |
| **Logit** | Unscaled output prediction value prior to sigmoid transformation. |
| **Ordinal Model** | Classifier designed for ordered categorical targets (Deleted < Undecided < Archived). |
| **Platt Scaling** | Logistic regression fitted on validation logits to calibrate probabilities. |
| **Regularisation ($C$)** | Penalty parameter constraining weight magnitude to prevent overfitting. |
| **SigLIP** | Pre-trained vision-language transformer model providing image embeddings. |
| **ViT** | Vision Transformer architecture processing images via patch tokens. |