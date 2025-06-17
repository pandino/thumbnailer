// Main app JavaScript file
document.addEventListener('DOMContentLoaded', function() {
    // Check for flash messages
    checkFlashMessages();

    // Load thumbnails for unviewed, error, and deleted sections
    loadThumbnails('unviewed-thumbnails', 'success', '0');
    loadThumbnails('error-thumbnails', 'error');
    loadThumbnails('deleted-thumbnails', 'deleted');

    // Handle keyboard shortcuts
    setupKeyboardShortcuts();
});

// Load thumbnails using AJAX
function loadThumbnails(containerId, status, viewed = null) {
    const container = document.getElementById(containerId);
    if (!container) return;

    // Clear loading indicator
    container.innerHTML = '<div class="loading">Loading...</div>';

    // Build API URL
    let url = `/api/thumbnails?status=${status}`;
    if (viewed !== null) {
        url += `&viewed=${viewed}`;
    }

    // Fetch thumbnails
    fetch(url)
        .then(response => {
            if (!response.ok) {
                throw new Error('Network response was not ok');
            }
            return response.json();
        })
        .then(thumbnails => {
            renderThumbnails(container, thumbnails);
        })
        .catch(error => {
            console.error('Error fetching thumbnails:', error);
            container.innerHTML = `<div class="error">Failed to load thumbnails: ${error.message}</div>`;
        });
}

function renderThumbnails(container, thumbnails) {
    if (!thumbnails || thumbnails.length === 0) {
        container.innerHTML = '<div class="no-results">No thumbnails found</div>';
        return;
    }

    // Clear container
    container.innerHTML = '';

    // Check if this is the deleted-thumbnails container
    const isDeletedContainer = container.id === 'deleted-thumbnails';
    
    // Sort by created_at or updated_at for more recent items if needed
    if (isDeletedContainer || container.id === 'unviewed-thumbnails') {
        thumbnails.sort((a, b) => new Date(b.updated_at) - new Date(a.updated_at));
        // Limit to 10 most recent items
        thumbnails = thumbnails.slice(0, 10);
    }

    // Add each thumbnail
    thumbnails.forEach(thumbnail => {
        const item = document.createElement('div');
        item.className = 'thumbnail-item';
        if (isDeletedContainer) {
            item.classList.add('pending-deletion');
        }
        
        // Add class based on the source
        if (thumbnail.source === 'imported') {
            item.classList.add('imported');
        }
        
        let itemContent = '';
        if (isDeletedContainer) {
            // For deleted items, don't link to slideshow but add undo button
            itemContent = `
                <div class="thumbnail-wrapper">
                    ${thumbnail.source === 'imported' ? '<span class="source-badge imported">Imported</span>' : ''}
                    <img src="/thumbnails/${thumbnail.thumbnail_path}" alt="${thumbnail.movie_filename}">
                    <div class="thumbnail-info">
                        <div class="thumbnail-title">${thumbnail.movie_filename}</div>
                        <div class="thumbnail-meta">
                            <span>${formatDuration(thumbnail.duration)}</span>
                            <span>${formatFileSize(thumbnail.file_size || 0)}</span>
                            <span>${formatDate(thumbnail.updated_at)}</span>
                        </div>
                    </div>
                    <button class="undo-delete-btn" data-thumbnail-id="${thumbnail.id}" title="Undo deletion">↩️ Undo</button>
                </div>
            `;
        } else {
            // For non-deleted items, link to slideshow
            itemContent = `
                <a href="/slideshow?id=${thumbnail.id}">
                    ${thumbnail.source === 'imported' ? '<span class="source-badge imported">Imported</span>' : ''}
                    <img src="/thumbnails/${thumbnail.thumbnail_path}" alt="${thumbnail.movie_filename}">
                    <div class="thumbnail-info">
                        <div class="thumbnail-title">${thumbnail.movie_filename}</div>
                        <div class="thumbnail-meta">
                            <span>${formatDuration(thumbnail.duration)}</span>
                            <span>${formatFileSize(thumbnail.file_size || 0)}</span>
                            <span>${formatDate(thumbnail.created_at)}</span>
                            ${thumbnail.source ? `<span class="source-label">${thumbnail.source}</span>` : ''}
                        </div>
                    </div>
                </a>
            `;
        }
        
        item.innerHTML = itemContent;
        container.appendChild(item);
    });

    // If this is the deleted items container, add event listeners for undo buttons
    if (isDeletedContainer) {
        const undoButtons = container.querySelectorAll('.undo-delete-btn');
        undoButtons.forEach(button => {
            button.addEventListener('click', function(e) {
                e.preventDefault();
                const thumbnailId = this.getAttribute('data-thumbnail-id');
                undoDeleteMovie(thumbnailId, this);
            });
        });
    }
}

// Add function to handle undo deletion
function undoDeleteMovie(thumbnailId, buttonElement) {
    // Disable the button while processing
    buttonElement.disabled = true;
    buttonElement.textContent = "Undoing...";
    
    // Send AJAX request to restore the movie
    fetch('/undo-delete', {
        method: 'POST',
        headers: {
            'Content-Type': 'application/x-www-form-urlencoded',
            'X-Requested-With': 'XMLHttpRequest'
        },
        body: `id=${encodeURIComponent(thumbnailId)}`
    })
    .then(response => {
        if (!response.ok) {
            throw new Error('Network response was not ok');
        }
        return response.json();
    })
    .then(data => {
        if (data.success) {
            // Reload the deleted thumbnails container
            loadThumbnails('deleted-thumbnails', 'deleted');
            // Reload the unviewed thumbnails container (the item will appear there)
            loadThumbnails('unviewed-thumbnails', 'success', '0');
            // Show success message
            showFlashMessage("Movie restored successfully");
        } else {
            throw new Error(data.message || 'Failed to restore movie');
        }
    })
    .catch(error => {
        console.error('Error undoing deletion:', error);
        buttonElement.disabled = false;
        buttonElement.textContent = "↩️ Undo";
        showFlashMessage("Failed to restore movie: " + error.message, "error");
    });
}


// Format duration in seconds to MM:SS or HH:MM:SS
function formatDuration(seconds) {
    if (!seconds) return '00:00';
    
    const hours = Math.floor(seconds / 3600);
    const minutes = Math.floor((seconds % 3600) / 60);
    const secs = Math.floor(seconds % 60);
    
    if (hours > 0) {
        return `${hours}:${padZero(minutes)}:${padZero(secs)}`;
    }
    return `${padZero(minutes)}:${padZero(secs)}`;
}

// Add leading zero to numbers less than 10
function padZero(num) {
    return num < 10 ? `0${num}` : num;
}

// Format file size from bytes to human readable format
function formatFileSize(bytes) {
    if (bytes === 0) return '0 B';
    
    const k = 1024;
    const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    
    return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i];
}

// Format date to relative time (e.g., "2 days ago")
function formatDate(dateString) {
    if (!dateString) return '';
    
    const date = new Date(dateString);
    const now = new Date();
    const diffMs = now - date;
    const diffDays = Math.floor(diffMs / (1000 * 60 * 60 * 24));
    
    if (diffDays === 0) {
        const diffHours = Math.floor(diffMs / (1000 * 60 * 60));
        if (diffHours === 0) {
            const diffMinutes = Math.floor(diffMs / (1000 * 60));
            return `${diffMinutes} min ago`;
        }
        return `${diffHours} hours ago`;
    } else if (diffDays === 1) {
        return 'Yesterday';
    } else if (diffDays < 7) {
        return `${diffDays} days ago`;
    } else {
        return `${date.toLocaleDateString()}`;
    }
}

// Check for flash messages (from cookies)
function checkFlashMessages() {
    const flashCookie = getCookie('flash');
    if (flashCookie) {
        showFlashMessage(decodeURIComponent(flashCookie));
        // Clear the cookie
        document.cookie = 'flash=; path=/; expires=Thu, 01 Jan 1970 00:00:00 GMT';
    }
}

// Show flash message
function showFlashMessage(message, type = '') {
    const flashDiv = document.createElement('div');
    flashDiv.className = 'flash-message';
    if (type) {
        flashDiv.classList.add(type);
    }
    flashDiv.textContent = message;
    
    document.body.appendChild(flashDiv);
    
    // Automatically remove after 5 seconds
    setTimeout(() => {
        flashDiv.classList.add('hiding');
        setTimeout(() => {
            flashDiv.remove();
        }, 500);
    }, 5000);
}

// Get cookie value by name
function getCookie(name) {
    const cookies = document.cookie.split(';');
    for (let i = 0; i < cookies.length; i++) {
        const cookie = cookies[i].trim();
        if (cookie.startsWith(name + '=')) {
            return cookie.substring(name.length + 1);
        }
    }
    return '';
}

// Set up keyboard shortcuts
function setupKeyboardShortcuts() {
    document.addEventListener('keydown', (e) => {
        // Check if we're in an input field
        if (e.target.tagName === 'INPUT' || e.target.tagName === 'TEXTAREA') {
            return;
        }
        
        // Shortcut: S - Start slideshow
        if (e.key === 's' || e.key === 'S') {
            const slideshowLink = document.querySelector('.start-slideshow');
            if (slideshowLink) {
                slideshowLink.click();
            }
        }
    });
}
