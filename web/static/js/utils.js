// Shared utilities for movie-thumbnailer web UI

function showFlashMessage(message, type = '') {
    const flashDiv = document.createElement('div');
    flashDiv.className = 'flash-message';
    if (type) flashDiv.classList.add(type);
    flashDiv.textContent = message;
    document.body.appendChild(flashDiv);
    setTimeout(() => {
        flashDiv.classList.add('hiding');
        setTimeout(() => flashDiv.remove(), 500);
    }, 5000);
}

// Shows a styled confirm modal. Returns a Promise that resolves true (confirmed) or false (cancelled).
function showConfirmModal(title, message, confirmLabel = 'Confirm', confirmColor = '#e74c3c') {
    return new Promise(resolve => {
        const overlay = document.createElement('div');
        overlay.className = 'modal-overlay';
        overlay.innerHTML = `
            <div class="modal-dialog">
                <div class="modal-title">${title}</div>
                <div class="modal-message">${message}</div>
                <div class="modal-buttons">
                    <button class="modal-btn modal-btn-cancel">Cancel</button>
                    <button class="modal-btn modal-btn-confirm" style="background-color:${confirmColor};color:white">${confirmLabel}</button>
                </div>
            </div>
        `;
        document.body.appendChild(overlay);

        overlay.querySelector('.modal-btn-cancel').addEventListener('click', () => {
            overlay.remove();
            resolve(false);
        });
        overlay.querySelector('.modal-btn-confirm').addEventListener('click', () => {
            overlay.remove();
            resolve(true);
        });
        overlay.addEventListener('click', e => {
            if (e.target === overlay) { overlay.remove(); resolve(false); }
        });
    });
}
