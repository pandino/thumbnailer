// Slideshow JavaScript file
document.addEventListener('DOMContentLoaded', function() {
    // Setup keyboard shortcuts
    setupKeyboardShortcuts();
    
    // Setup forms for AJAX submission
    setupAjaxForms();
    
    // Set focus to the page for keyboard shortcuts
    document.body.focus();
});

// Setup keyboard shortcuts for slideshow navigation
function setupKeyboardShortcuts() {
    document.addEventListener('keydown', function(e) {
        // Prevent default behavior for navigation keys
        if ([' ', 'ArrowRight', 'u', 'U', 'd', 'D', 'Escape', 's', 'S'].includes(e.key)) {
            e.preventDefault();
            
            // Handle different keys
            switch (e.key) {
                case ' ':
                case 'ArrowRight':
                    // Next thumbnail (now marks as viewed)
                    navigateToNext();
                    break;
                
                case 'u':
                case 'U':
                    // Undo - only if not disabled
                    if (!isUndoDisabled()) {
                        navigateToUndo();
                    }
                    break;
                
                case 'd':
                case 'D':
                    // Delete thumbnail (without confirmation)
                    deleteMovie();
                    break;
                
                case 's':
                case 'S':
                    // Skip to next thumbnail without marking as viewed
                    skipToNext();
                    break;
                
                case 'Escape':
                    // Back to control page
                    window.location.href = '/';
                    break;
            }
        }
    });
}

// Check if undo button is disabled
function isUndoDisabled() {
    const undoButton = document.querySelector('.nav-button.undo');
    return undoButton && undoButton.classList.contains('disabled');
}

// Navigate to next thumbnail
function navigateToNext() {
    const nextButton = document.querySelector('.nav-button.next');
    if (nextButton) {
        nextButton.click();
        // Preload the next image after navigation
        setTimeout(preloadNextImage, 1000);
    }
}

// Navigate to undo (previous) thumbnail
function navigateToUndo() {
    const undoButton = document.querySelector('.nav-button.undo');
    if (undoButton && !undoButton.classList.contains('disabled')) {
        undoButton.click();
        // Preload the next image after navigation
        setTimeout(preloadNextImage, 1000);
    }
}

// Skip to next thumbnail without marking as viewed
function skipToNext() {
    // Get current thumbnail ID
    const currentThumbnailId = getCurrentThumbnailId();
    if (currentThumbnailId) {
        window.location.href = `/slideshow/next?current=${currentThumbnailId}&skip=true`;
        // Preload the next image after navigation
        setTimeout(preloadNextImage, 1000);
    }
}

// Get current thumbnail ID from the page
function getCurrentThumbnailId() {
    // Look for the current thumbnail ID in the page - we can get it from the next button href
    const nextButton = document.querySelector('.nav-button.next');
    if (nextButton && nextButton.href) {
        const url = new URL(nextButton.href);
        return url.searchParams.get('current');
    }
    return null;
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
    
    // Disable clicks on disabled Undo button
    const undoButton = document.querySelector('.nav-button.undo.disabled');
    if (undoButton) {
        undoButton.addEventListener('click', function(e) {
            e.preventDefault();
            return false;
        });
    }
}

// Submit form via AJAX
function submitFormAjax(form, callback) {
    const formData = new FormData(form);
    const xhr = new XMLHttpRequest();
    
    xhr.open('POST', form.action, true);
    xhr.setRequestHeader('X-Requested-With', 'XMLHttpRequest');
    
    xhr.onload = function() {
        if (xhr.status >= 200 && xhr.status < 400) {
            // Success - check if server wants us to redirect
            try {
                const response = JSON.parse(xhr.responseText);
                if (response.redirect) {
                    window.location.href = response.redirect;
                    return;
                }
            } catch (e) {
                // Not JSON or no redirect, continue with callback
            }
            
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
    // Only preload if we have an active slideshow session
    fetch('/api/slideshow/next-image', {
        method: 'GET',
        credentials: 'same-origin' // Include cookies
    })
    .then(response => {
        if (response.ok) {
            return response.json();
        }
        // Silently ignore errors to not break the UI
        return null;
    })
    .then(data => {
        if (data && data.hasNext && data.thumbnailPath) {
            // Create a new Image object to preload the thumbnail
            const img = new Image();
            img.src = '/thumbnails/' + data.thumbnailPath;
            
            // Store reference to prevent garbage collection
            window.preloadedImage = img;
            
            console.debug('Preloaded next image:', data.movieFilename);
        } else {
            console.debug('No next image to preload');
        }
    })
    .catch(error => {
        // Silently log error - don't break the UI
        console.debug('Failed to preload next image:', error);
    });
}

// Call preload function when page loads
setTimeout(preloadNextImage, 1000);
