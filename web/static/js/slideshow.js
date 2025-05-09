// Slideshow JavaScript file
document.addEventListener('DOMContentLoaded', function() {
    // Setup keyboard shortcuts
    setupKeyboardShortcuts();
    
    // Setup forms for AJAX submission
    setupAjaxForms();
    
    // Set focus to the page for keyboard shortcuts
    document.body.focus();
    
    // Set up history timeline animation
    animateHistoryTimeline();
});

// Setup keyboard shortcuts for slideshow navigation
function setupKeyboardShortcuts() {
    document.addEventListener('keydown', function(e) {
        // Prevent default behavior for navigation keys
        if ([' ', 'ArrowRight', 'ArrowLeft', 'm', 'M', 'd', 'D', 'Escape', 'r', 'R'].includes(e.key)) {
            e.preventDefault();
            
            // Handle different keys
            switch (e.key) {
                case ' ':
                case 'ArrowRight':
                    // Next thumbnail
                    navigateToNext();
                    break;
                
                case 'ArrowLeft':
                    // Previous thumbnail - only if not disabled
                    if (!isPreviousDisabled()) {
                        navigateToPrevious();
                    }
                    break;
                
                case 'm':
                case 'M':
                    // Mark as viewed
                    markAsViewed();
                    break;
                
                case 'd':
                case 'D':
                    // Delete thumbnail (without confirmation)
                    deleteMovie();
                    break;
                
                case 'r':
                case 'R':
                    // Reset history
                    resetHistory();
                    break;
                
                case 'Escape':
                    // Back to control page
                    window.location.href = '/';
                    break;
            }
        }
    });
}

// Check if previous button is disabled
function isPreviousDisabled() {
    const prevButton = document.querySelector('.nav-button.prev');
    return prevButton && prevButton.classList.contains('disabled');
}

// Navigate to next thumbnail
function navigateToNext() {
    const nextButton = document.querySelector('.nav-button.next');
    if (nextButton) {
        nextButton.click();
    }
}

// Navigate to previous thumbnail
function navigateToPrevious() {
    const prevButton = document.querySelector('.nav-button.prev');
    if (prevButton && !prevButton.classList.contains('disabled')) {
        prevButton.click();
    }
}

// Reset history
function resetHistory() {
    const resetButton = document.querySelector('.nav-button.reset-history');
    if (resetButton) {
        resetButton.click();
    }
}

// Mark current thumbnail as viewed
function markAsViewed() {
    const form = document.getElementById('mark-viewed-form');
    if (form) {
        submitFormAjax(form, function() {
            // Navigate to next after marking as viewed
            navigateToNext();
        });
    }
}

// Delete movie without confirmation
function deleteMovie() {
    const form = document.getElementById('delete-form');
    if (form) {
        const buttonElement = form.querySelector('button');
        
        // Only process if the button is not disabled
        if (buttonElement && !buttonElement.disabled) {
            submitFormAjax(form, function() {
                // Navigate to next after marking for deletion
                navigateToNext();
            });
        }
    }
}

// Setup form AJAX submissions
function setupAjaxForms() {
    // Mark as viewed form
    const viewForm = document.getElementById('mark-viewed-form');
    if (viewForm) {
        viewForm.addEventListener('submit', function(e) {
            e.preventDefault();
            submitFormAjax(this, function() {
                // Update session display counter before navigating to next
                const counterEl = document.querySelector('.slideshow-counter');
                if (counterEl) {
                    const counterText = counterEl.textContent;
                    const match = counterText.match(/Thumbnail (\d+) of (\d+)/);
                    if (match && match.length >= 3) {
                        const current = parseInt(match[1]);
                        const total = parseInt(match[2]);
                        counterEl.textContent = `Thumbnail ${current + 1} of ${total}`;
                        
                        // Make sure we keep the random indicator
                        if (!counterEl.querySelector('.random-indicator')) {
                            const randomIndicator = document.createElement('span');
                            randomIndicator.className = 'random-indicator';
                            randomIndicator.textContent = 'Random Order';
                            counterEl.appendChild(randomIndicator);
                        }
                    }
                }
                
                // Navigate to next after marking as viewed
                navigateToNext();
            });
        });
    }
    
    // Delete form - no confirmation
    const deleteForm = document.getElementById('delete-form');
    if (deleteForm) {
        deleteForm.addEventListener('submit', function(e) {
            e.preventDefault();
            submitFormAjax(this, function() {
                // Navigate to next after marking for deletion
                navigateToNext();
            });
        });
    }
    
    // Disable clicks on disabled Previous button
    const prevButton = document.querySelector('.nav-button.prev.disabled');
    if (prevButton) {
        prevButton.addEventListener('click', function(e) {
            e.preventDefault();
            return false;
        });
    }
}

// Animate history timeline dots
function animateHistoryTimeline() {
    const dots = document.querySelectorAll('.history-dot');
    dots.forEach((dot, index) => {
        setTimeout(() => {
            dot.style.opacity = '0';
            setTimeout(() => {
                dot.style.opacity = '1';
            }, 200);
        }, index * 100);
    });
}

// Submit form via AJAX
function submitFormAjax(form, callback) {
    const formData = new FormData(form);
    const xhr = new XMLHttpRequest();
    
    xhr.open('POST', form.action, true);
    xhr.setRequestHeader('X-Requested-With', 'XMLHttpRequest');
    
    xhr.onload = function() {
        if (xhr.status >= 200 && xhr.status < 400) {
            // Success
            if (callback) {
                callback();
            }
        } else {
            // Error
            console.error('Form submission failed:', xhr.statusText);
            alert('Action failed: ' + xhr.statusText);
        }
    };
    
    xhr.onerror = function() {
        console.error('Network error during form submission');
        alert('Network error. Please try again.');
    };
    
    xhr.send(formData);
}

// Preload next image for smoother navigation
function preloadNextImage() {
    const nextLink = document.querySelector('.nav-button.next');
    if (nextLink) {
        fetch(nextLink.href)
            .then(response => response.text())
            .then(html => {
                const parser = new DOMParser();
                const doc = parser.parseFromString(html, 'text/html');
                const nextImageSrc = doc.querySelector('.thumbnail-image')?.src;
                
                if (nextImageSrc) {
                    const preloadImage = new Image();
                    preloadImage.src = nextImageSrc;
                }
            })
            .catch(error => {
                console.error('Error preloading next image:', error);
            });
    }
}

// Call preload function when page loads
setTimeout(preloadNextImage, 1000);
