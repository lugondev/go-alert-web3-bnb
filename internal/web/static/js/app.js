// Web3 Alert - Main JavaScript

// HTMX event handlers
document.addEventListener('htmx:afterSwap', function(event) {
    // Handle alerts after HTMX swap
    const alertContainer = document.getElementById('alert-container');
    if (alertContainer && alertContainer.children.length > 0) {
        setTimeout(() => {
            alertContainer.innerHTML = '';
        }, 5000);
    }
});

document.addEventListener('htmx:responseError', function(event) {
    showNotification('error', 'An error occurred. Please try again.');
});

// Utility functions
function showNotification(type, message) {
    const container = document.getElementById('alert-container');
    if (!container) return;

    const alertClass = type === 'error' ? 'bg-red-900 text-red-200' : 
                       type === 'success' ? 'bg-green-900 text-green-200' :
                       type === 'warning' ? 'bg-yellow-900 text-yellow-200' :
                       'bg-blue-900 text-blue-200';

    const alert = document.createElement('div');
    alert.className = `p-4 rounded-md mb-4 alert-enter ${alertClass}`;
    alert.innerHTML = `
        <div class="flex items-center justify-between">
            <span>${message}</span>
            <button onclick="this.parentElement.parentElement.remove()" class="ml-4 hover:opacity-75">
                <svg class="w-4 h-4" fill="currentColor" viewBox="0 0 20 20">
                    <path fill-rule="evenodd" d="M4.293 4.293a1 1 0 011.414 0L10 8.586l4.293-4.293a1 1 0 111.414 1.414L11.414 10l4.293 4.293a1 1 0 01-1.414 1.414L10 11.414l-4.293 4.293a1 1 0 01-1.414-1.414L8.586 10 4.293 5.707a1 1 0 010-1.414z" clip-rule="evenodd"></path>
                </svg>
            </button>
        </div>
    `;

    container.appendChild(alert);

    // Auto remove after 5 seconds
    setTimeout(() => {
        alert.classList.add('alert-exit');
        setTimeout(() => alert.remove(), 300);
    }, 5000);
}

// Confirm dialog helper
function confirmAction(message) {
    return confirm(message);
}

// Copy to clipboard
function copyToClipboard(text) {
    navigator.clipboard.writeText(text).then(() => {
        showNotification('success', 'Copied to clipboard');
    }).catch(() => {
        showNotification('error', 'Failed to copy');
    });
}

// Format address for display
function formatAddress(address, length = 8) {
    if (!address || address.length <= length * 2) return address;
    return address.slice(0, length) + '...' + address.slice(-length);
}

// API helper functions
async function apiCall(url, method = 'GET', data = null) {
    const options = {
        method,
        headers: {
            'Content-Type': 'application/json',
        },
    };

    if (data && method !== 'GET') {
        options.body = JSON.stringify(data);
    }

    try {
        const response = await fetch(url, options);
        const result = await response.json();

        if (!response.ok) {
            throw new Error(result.error || 'Request failed');
        }

        return result;
    } catch (error) {
        showNotification('error', error.message);
        throw error;
    }
}

// Token management functions
async function deleteToken(id) {
    if (!confirmAction('Are you sure you want to delete this token?')) {
        return;
    }

    try {
        await apiCall(`/api/tokens/${id}`, 'DELETE');
        const row = document.getElementById(`token-row-${id}`);
        if (row) {
            row.remove();
        }
        showNotification('success', 'Token deleted successfully');
    } catch (error) {
        // Error already shown by apiCall
    }
}

async function toggleNotify(id, enabled) {
    try {
        await apiCall(`/api/tokens/${id}/notify`, 'POST', { enabled });
        location.reload();
    } catch (error) {
        // Error already shown by apiCall
    }
}

// Connection status checker
let connectionCheckInterval;

function startConnectionChecker() {
    connectionCheckInterval = setInterval(async () => {
        try {
            const response = await fetch('/health');
            updateConnectionStatus(response.ok);
        } catch {
            updateConnectionStatus(false);
        }
    }, 30000); // Check every 30 seconds
}

function updateConnectionStatus(connected) {
    const statusEl = document.getElementById('connection-status');
    if (!statusEl) return;

    if (connected) {
        statusEl.innerHTML = `
            <span class="inline-block w-2 h-2 bg-green-500 rounded-full mr-2 status-pulse"></span>
            Connected
        `;
    } else {
        statusEl.innerHTML = `
            <span class="inline-block w-2 h-2 bg-red-500 rounded-full mr-2"></span>
            Disconnected
        `;
    }
}

// Active nav link highlighter
function highlightActiveNav() {
    const path = window.location.pathname;
    const navLinks = document.querySelectorAll('.nav-link');

    navLinks.forEach(link => {
        link.classList.remove('active', 'bg-gray-700', 'text-white');
        
        const href = link.getAttribute('href');
        if (href === path || (href !== '/' && path.startsWith(href))) {
            link.classList.add('active', 'bg-gray-700', 'text-white');
        }
    });
}

// Initialize on page load
document.addEventListener('DOMContentLoaded', function() {
    highlightActiveNav();
    startConnectionChecker();
});

// Cleanup on page unload
window.addEventListener('beforeunload', function() {
    if (connectionCheckInterval) {
        clearInterval(connectionCheckInterval);
    }
});
