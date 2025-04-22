// Main app JavaScript file
document.addEventListener('DOMContentLoaded', function() {
    // Check for flash messages
    checkFlashMessages();

    // Load thumbnails for unviewed and error sections
    loadThumbnails('unviewed-thumbnails', 'success', '0');
    loadThumbnails('error-thumbnails', 'error');

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

// Render thumbnails in the container
function renderThumbnails(container, thumbnails) {
    if (!thumbnails || thumbnails.length === 0) {
        container.innerHTML = '<div class="no-results">No thumbnails found</div>';
        return;
    }

    // Clear container
    container.innerHTML = '';

    // Limit to first 12 thumbnails for performance
    const displayThumbnails = thumbnails.slice(0, 12);

    // Add each thumbnail
    displayThumbnails.forEach(thumbnail => {
        const item = document.createElement('div');
        item.className = 'thumbnail-item';
        
        // Create thumbnail content
        item.innerHTML = `
            <a href="/slideshow?id=${thumbnail.id}">
                <img src="/thumbnails/${thumbnail.thumbnail_path}" alt="${thumbnail.movie_filename}">
                <div class="thumbnail-info">
                    <div class="thumbnail-title">${thumbnail.movie_filename}</div>
                    <div class="thumbnail-meta">
                        <span>${formatDuration(thumbnail.duration)}</span>
                        <span>${formatDate(thumbnail.created_at)}</span>
                    </div>
                </div>
            </a>
        `;
        
        container.appendChild(item);
    });

    // Add "Show All" link if there are more thumbnails
    if (thumbnails.length > 12) {
        const showMore = document.createElement('div');
        showMore.className = 'thumbnail-item show-more';
        showMore.innerHTML = `
            <div class="show-more-content">
                <span>+ ${thumbnails.length - 12} more</span>
                <button>Show All</button>
            </div>
        `;
        container.appendChild(showMore);
    }
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
function showFlashMessage(message) {
    const flashDiv = document.createElement('div');
    flashDiv.className = 'flash-message';
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
