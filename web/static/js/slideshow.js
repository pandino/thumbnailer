// Slideshow JavaScript file
document.addEventListener('DOMContentLoaded', function() {
    // Setup keyboard shortcuts
    setupKeyboardShortcuts();

    // Setup forms for AJAX submission
    setupAjaxForms();

    // Setup shortcuts help modal
    setupShortcutsHelp();

    // Set focus to the page for keyboard shortcuts
    document.body.focus();
});

// Setup keyboard shortcuts for slideshow navigation
function setupKeyboardShortcuts() {
    document.addEventListener('keydown', function(e) {
        // Prevent default behavior for navigation keys
        if ([' ', 'ArrowRight', 'u', 'U', 'd', 'D', 'm', 'M', 'Escape', 's', 'S', '?'].includes(e.key)) {
            e.preventDefault();

            switch (e.key) {
                case ' ':
                case 'ArrowRight':
                    navigateToNext();
                    break;

                case 'u':
                case 'U':
                    if (!isUndoDisabled()) {
                        navigateToUndo();
                    }
                    break;

                case 'd':
                case 'D':
                    deleteMovie();
                    break;

                case 'm':
                case 'M':
                    archiveMovie();
                    break;

                case 's':
                case 'S':
                    skipToNext();
                    break;

                case 'Escape':
                    window.location.href = '/';
                    break;

                case '?':
                    toggleShortcutsHelp();
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

// Navigate to next thumbnail or finish if last
function navigateToNext() {
    const nextButton = document.querySelector('.nav-button.next');
    const finishButton = document.querySelector('.nav-button.finish');
    
    if (finishButton) {
        // This is the last thumbnail, finish the slideshow
        finishButton.click();
    } else if (nextButton) {
        nextButton.click();
        // Only preload if not finishing
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
    const finishButton = document.querySelector('.nav-button.finish');
    if (finishButton) {
        showFlashMessage('Last slide — use Finish or Delete & Finish to end the session.', 'warning');
        return;
    }
    window.location.href = '/slideshow/next?skip=true';
    setTimeout(preloadNextImage, 1000);
}

// Delete movie without confirmation
function deleteMovie() {
    const form = document.querySelector('form[action*="delete"]');
    if (form) {
        const buttonElement = form.querySelector('button');
        
        // Only process if the button is not disabled
        if (buttonElement && !buttonElement.disabled) {
            // Check if this is a delete-and-finish form (last thumbnail)
            const isDeleteAndFinish = form.action.includes('delete-and-finish');
            
            if (isDeleteAndFinish) {
                // For last thumbnail, submit directly (will redirect to home)
                submitFormAjax(form, function(response) {
                    if (response && response.redirect) {
                        window.location.href = response.redirect;
                    } else {
                        window.location.href = '/';
                    }
                });
            } else {
                // For normal deletion, navigate to next
                submitFormAjax(form, function() {
                    navigateToNext();
                });
            }
        }
    }
}

// Archive movie without confirmation
function archiveMovie() {
    const form = document.querySelector('form[action*="archive"]');
    if (form) {
        const buttonElement = form.querySelector('button');
        
        // Only process if the button is not disabled
        if (buttonElement && !buttonElement.disabled) {
            // For archival, always navigate to next
            submitFormAjax(form, function() {
                navigateToNext();
            });
        }
    }
}

// Setup form AJAX submissions
function setupAjaxForms() {
    // Delete forms - no confirmation
    const deleteForms = document.querySelectorAll('form[action*="delete"]');
    deleteForms.forEach(form => {
        form.addEventListener('submit', function(e) {
            e.preventDefault();
            
            // Check if this is a delete-and-finish form (last thumbnail)
            const isDeleteAndFinish = this.action.includes('delete-and-finish');
            
            if (isDeleteAndFinish) {
                // For last thumbnail, submit directly (will redirect to home)
                submitFormAjax(this, function(response) {
                    if (response && response.redirect) {
                        window.location.href = response.redirect;
                    } else {
                        window.location.href = '/';
                    }
                });
            } else {
                // For normal deletion, navigate to next
                submitFormAjax(this, function() {
                    navigateToNext();
                });
            }
        });
    });
    
    // Archive forms
    const archiveForms = document.querySelectorAll('form[action*="archive"]');
    archiveForms.forEach(form => {
        form.addEventListener('submit', function(e) {
            e.preventDefault();
            
            // For archival, always navigate to next
            submitFormAjax(this, function() {
                navigateToNext();
            });
        });
    });
    
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
            let response = null;
            try {
                if (xhr.responseText) response = JSON.parse(xhr.responseText);
            } catch (e) {
                // not JSON, that's fine
            }
            if (callback) callback(response);
        } else {
            console.error('Form submission failed:', xhr.statusText);
            showFlashMessage('Action failed: ' + xhr.statusText, 'error');
        }
    };

    xhr.onerror = function() {
        console.error('Network error during form submission');
        showFlashMessage('Network error. Please try again.', 'error');
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

// Shortcuts help modal
function setupShortcutsHelp() {
    const helpBtn = document.getElementById('shortcuts-help-btn');
    const modal   = document.getElementById('shortcuts-modal');
    if (!helpBtn || !modal) return;

    helpBtn.addEventListener('click', () => toggleShortcutsHelp());
    document.getElementById('shortcuts-modal-close').addEventListener('click', () => {
        modal.style.display = 'none';
    });
    modal.addEventListener('click', e => {
        if (e.target === modal) modal.style.display = 'none';
    });
}

function toggleShortcutsHelp() {
    const modal = document.getElementById('shortcuts-modal');
    if (!modal) return;
    modal.style.display = modal.style.display === 'flex' ? 'none' : 'flex';
}
