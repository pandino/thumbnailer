FROM docker.io/golang:1.24-alpine AS builder

WORKDIR /app

# Install build dependencies including gcc and musl-dev
RUN apk add --no-cache git gcc musl-dev

# Copy go.mod and go.sum files
COPY go.mod go.sum* ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Add build arguments
ARG VERSION="0.0.0-dev"
ARG COMMIT="unknown"
ARG BUILD_DATE="unknown"

# Add a version.go file to store version information
RUN printf 'package main\n\nvar (\n    version   = "%s"\n    commit    = "%s"\n    buildDate = "%s"\n)\n' \
    "$VERSION" "$COMMIT" "$BUILD_DATE" > ./cmd/movie-thumbnailer/version.go

# Build the application
RUN --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=1 GOOS=linux go build -o movie-thumbnailer ./cmd/movie-thumbnailer

FROM docker.io/alpine:3.21

LABEL maintainer="Movie Thumbnailer"
LABEL description="A Go application for generating movie thumbnail mosaics with web interface"
LABEL version="${VERSION}"
LABEL commit="${COMMIT}"
LABEL build_date="${BUILD_DATE}"

# Install runtime dependencies
RUN apk add --no-cache \
    ffmpeg \
    sqlite \
    ca-certificates \
    tzdata \
    dumb-init

# Set up non-root user
RUN addgroup -g 1000 thumbnailer && \
    adduser -u 1000 -G thumbnailer -s /bin/sh -D thumbnailer

# Set up directories
RUN mkdir -p /app /movies /thumbnails /data \
    && chown -R thumbnailer:thumbnailer /app /thumbnails /data

# Copy compiled application from builder stage
COPY --from=builder --chown=thumbnailer:thumbnailer /app/movie-thumbnailer /app/

# Copy web assets
COPY --from=builder --chown=thumbnailer:thumbnailer /app/web /app/web

# Set working directory
WORKDIR /app

# Switch to non-root user
USER thumbnailer

# Set default environment variables
ENV MOVIE_INPUT_DIR=/movies
ENV THUMBNAIL_OUTPUT_DIR=/thumbnails
ENV DATA_DIR=/data
ENV SERVER_PORT=8080
ENV TEMPLATES_DIR=/app/web/templates
ENV STATIC_DIR=/app/web/static

# Volume configuration
VOLUME ["/movies", "/thumbnails", "/data"]

# Expose server port
EXPOSE 8080

# Use dumb-init as entrypoint to handle signals properly
ENTRYPOINT ["/usr/bin/dumb-init", "--", "/app/movie-thumbnailer"]
