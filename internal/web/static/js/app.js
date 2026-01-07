document.addEventListener('htmx:afterSwap', function(event) {
    const alertContainer = document.getElementById('alert-container');
    if (alertContainer && alertContainer.children.length > 0) {
        setTimeout(() => {
            alertContainer.innerHTML = '';
        }, 5000);
    }
});

document.addEventListener('htmx:responseError', function(event) {
    showNotification('error', '[ERROR] Request failed. Please try again.');
});

function showNotification(type, message) {
    const container = document.getElementById('alert-container');
    if (!container) return;

    const alertClass = type === 'error' ? 'bg-cyber-red/20 text-cyber-red border-cyber-red' : 
                       type === 'success' ? 'bg-cyber-green/20 text-cyber-green border-cyber-green' :
                       type === 'warning' ? 'bg-cyber-amber/20 text-cyber-amber border-cyber-amber' :
                       'bg-cyber-cyan/20 text-cyber-cyan border-cyber-cyan';

    const alert = document.createElement('div');
    alert.className = `p-4 border-2 mb-3 alert-enter font-mono text-sm ${alertClass}`;
    alert.innerHTML = `
        <div class="flex items-center justify-between">
            <span class="font-bold">${message}</span>
            <button onclick="this.parentElement.parentElement.remove()" class="ml-4 hover:opacity-75 font-bold text-lg">
                ✕
            </button>
        </div>
    `;

    container.appendChild(alert);

    setTimeout(() => {
        alert.classList.add('alert-exit');
        setTimeout(() => alert.remove(), 300);
    }, 5000);
}

// Custom Confirm Modal
function confirmAction(message) {
    return new Promise((resolve) => {
        const modalId = 'confirm-modal-' + Date.now();
        const modal = document.createElement('div');
        modal.id = modalId;
        modal.className = 'fixed inset-0 bg-black/90 backdrop-blur-sm z-[9999] flex items-center justify-center p-4';
        modal.innerHTML = `
            <div class="bg-cyber-dark border-2 border-cyber-amber max-w-md w-full animate-slide-down shadow-2xl shadow-cyber-amber/20">
                <div class="border-b-2 border-cyber-amber bg-cyber-darker px-6 py-4">
                    <div class="flex items-center space-x-3">
                        <svg class="w-6 h-6 text-cyber-amber" fill="currentColor" viewBox="0 0 20 20">
                            <path fill-rule="evenodd" d="M8.257 3.099c.765-1.36 2.722-1.36 3.486 0l5.58 9.92c.75 1.334-.213 2.98-1.742 2.98H4.42c-1.53 0-2.493-1.646-1.743-2.98l5.58-9.92zM11 13a1 1 0 11-2 0 1 1 0 012 0zm-1-8a1 1 0 00-1 1v3a1 1 0 002 0V6a1 1 0 00-1-1z" clip-rule="evenodd"/>
                        </svg>
                        <h3 class="text-lg font-bold text-cyber-amber uppercase tracking-wide">[CONFIRM_ACTION]</h3>
                    </div>
                </div>
                <div class="p-6">
                    <p class="text-white font-mono text-sm leading-relaxed">${message}</p>
                </div>
                <div class="flex items-center justify-end space-x-4 px-6 pb-6">
                    <button data-action="cancel" 
                        class="px-6 py-3 bg-cyber-gray-800 text-white font-bold text-xs uppercase tracking-wider hover:bg-cyber-gray-700 transition-all border-2 border-cyber-gray-600">
                        Cancel
                    </button>
                    <button data-action="confirm" 
                        class="px-6 py-3 bg-cyber-amber text-cyber-black font-bold text-xs uppercase tracking-wider hover:bg-cyber-red hover:shadow-lg hover:shadow-cyber-red/50 transition-all border-2 border-cyber-amber">
                        Confirm
                    </button>
                </div>
            </div>
        `;
        
        document.body.appendChild(modal);
        
        const cleanup = () => {
            modal.classList.add('opacity-0');
            modal.classList.add('transition-opacity');
            setTimeout(() => modal.remove(), 200);
        };
        
        modal.querySelector('[data-action="confirm"]').addEventListener('click', () => {
            cleanup();
            resolve(true);
        });
        
        modal.querySelector('[data-action="cancel"]').addEventListener('click', () => {
            cleanup();
            resolve(false);
        });
        
        modal.addEventListener('click', (e) => {
            if (e.target === modal) {
                cleanup();
                resolve(false);
            }
        });
        
        document.addEventListener('keydown', function escHandler(e) {
            if (e.key === 'Escape') {
                cleanup();
                resolve(false);
                document.removeEventListener('keydown', escHandler);
            }
        });
        
        setTimeout(() => modal.querySelector('[data-action="confirm"]').focus(), 100);
    });
}

// Custom Alert Modal
function showAlert(message, type = 'error') {
    return new Promise((resolve) => {
        const modalId = 'alert-modal-' + Date.now();
        const modal = document.createElement('div');
        modal.id = modalId;
        modal.className = 'fixed inset-0 bg-black/90 backdrop-blur-sm z-[9999] flex items-center justify-center p-4';
        
        const typeConfig = {
            error: {
                color: 'cyber-red',
                icon: '<path fill-rule="evenodd" d="M10 18a8 8 0 100-16 8 8 0 000 16zM8.707 7.293a1 1 0 00-1.414 1.414L8.586 10l-1.293 1.293a1 1 0 101.414 1.414L10 11.414l1.293 1.293a1 1 0 001.414-1.414L11.414 10l1.293-1.293a1 1 0 00-1.414-1.414L10 8.586 8.707 7.293z" clip-rule="evenodd"/>',
                title: '[ERROR]'
            },
            success: {
                color: 'cyber-green',
                icon: '<path fill-rule="evenodd" d="M10 18a8 8 0 100-16 8 8 0 000 16zm3.707-9.293a1 1 0 00-1.414-1.414L9 10.586 7.707 9.293a1 1 0 00-1.414 1.414l2 2a1 1 0 001.414 0l4-4z" clip-rule="evenodd"/>',
                title: '[SUCCESS]'
            },
            warning: {
                color: 'cyber-amber',
                icon: '<path fill-rule="evenodd" d="M8.257 3.099c.765-1.36 2.722-1.36 3.486 0l5.58 9.92c.75 1.334-.213 2.98-1.742 2.98H4.42c-1.53 0-2.493-1.646-1.743-2.98l5.58-9.92zM11 13a1 1 0 11-2 0 1 1 0 012 0zm-1-8a1 1 0 00-1 1v3a1 1 0 002 0V6a1 1 0 00-1-1z" clip-rule="evenodd"/>',
                title: '[WARNING]'
            },
            info: {
                color: 'cyber-cyan',
                icon: '<path fill-rule="evenodd" d="M18 10a8 8 0 11-16 0 8 8 0 0116 0zm-7-4a1 1 0 11-2 0 1 1 0 012 0zM9 9a1 1 0 000 2v3a1 1 0 001 1h1a1 1 0 100-2v-3a1 1 0 00-1-1H9z" clip-rule="evenodd"/>',
                title: '[INFO]'
            }
        };
        
        const config = typeConfig[type] || typeConfig.error;
        
        modal.innerHTML = `
            <div class="bg-cyber-dark border-2 border-${config.color} max-w-md w-full animate-slide-down shadow-2xl shadow-${config.color}/20">
                <div class="border-b-2 border-${config.color} bg-cyber-darker px-6 py-4">
                    <div class="flex items-center space-x-3">
                        <svg class="w-6 h-6 text-${config.color}" fill="currentColor" viewBox="0 0 20 20">
                            ${config.icon}
                        </svg>
                        <h3 class="text-lg font-bold text-${config.color} uppercase tracking-wide">${config.title}</h3>
                    </div>
                </div>
                <div class="p-6">
                    <p class="text-white font-mono text-sm leading-relaxed">${message}</p>
                </div>
                <div class="flex items-center justify-end px-6 pb-6">
                    <button data-action="close" 
                        class="px-6 py-3 bg-${config.color} text-cyber-black font-bold text-xs uppercase tracking-wider hover:shadow-lg hover:shadow-${config.color}/50 transition-all border-2 border-${config.color}">
                        OK
                    </button>
                </div>
            </div>
        `;
        
        document.body.appendChild(modal);
        
        const cleanup = () => {
            modal.classList.add('opacity-0');
            modal.classList.add('transition-opacity');
            setTimeout(() => modal.remove(), 200);
        };
        
        modal.querySelector('[data-action="close"]').addEventListener('click', () => {
            cleanup();
            resolve();
        });
        
        modal.addEventListener('click', (e) => {
            if (e.target === modal) {
                cleanup();
                resolve();
            }
        });
        
        document.addEventListener('keydown', function escHandler(e) {
            if (e.key === 'Escape' || e.key === 'Enter') {
                cleanup();
                resolve();
                document.removeEventListener('keydown', escHandler);
            }
        });
        
        setTimeout(() => modal.querySelector('[data-action="close"]').focus(), 100);
    });
}

function copyToClipboard(text) {
    navigator.clipboard.writeText(text).then(() => {
        showNotification('success', '[SUCCESS] Copied to clipboard');
    }).catch(() => {
        showNotification('error', '[ERROR] Failed to copy');
    });
}

function formatAddress(address, length = 8) {
    if (!address || address.length <= length * 2) return address;
    return address.slice(0, length) + '...' + address.slice(-length);
}

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
            throw new Error(result.error || '[ERROR] Request failed');
        }

        return result;
    } catch (error) {
        showNotification('error', error.message);
        throw error;
    }
}

async function deleteToken(id) {
    const confirmed = await confirmAction('[CONFIRM] Delete this token? This action cannot be undone.');
    if (!confirmed) {
        return;
    }

    try {
        await apiCall(`/api/tokens/${id}`, 'DELETE');
        const row = document.getElementById(`token-row-${id}`);
        if (row) {
            row.style.opacity = '0';
            row.style.transform = 'translateX(-20px)';
            setTimeout(() => row.remove(), 300);
        }
        showNotification('success', '[SUCCESS] Token deleted');
    } catch (error) {
    }
}

async function toggleNotify(id, enabled) {
    try {
        await apiCall(`/api/tokens/${id}/notify`, 'POST', { enabled });
        location.reload();
    } catch (error) {
    }
}

let connectionCheckInterval;

function startConnectionChecker() {
    connectionCheckInterval = setInterval(async () => {
        try {
            const response = await fetch('/health');
            updateConnectionStatus(response.ok);
        } catch {
            updateConnectionStatus(false);
        }
    }, 30000);
}

function updateConnectionStatus(connected) {
    const statusEl = document.getElementById('connection-status');
    if (!statusEl) return;

    if (connected) {
        statusEl.innerHTML = `
            <span class="inline-block w-2 h-2 bg-cyber-green rounded-full status-pulse"></span>
            <span class="text-xs font-bold text-cyber-green uppercase tracking-wider">ONLINE</span>
        `;
    } else {
        statusEl.innerHTML = `
            <span class="inline-block w-2 h-2 bg-cyber-red rounded-full"></span>
            <span class="text-xs font-bold text-cyber-red uppercase tracking-wider">OFFLINE</span>
        `;
    }
}

function highlightActiveNav() {
    const path = window.location.pathname;
    const navLinks = document.querySelectorAll('.nav-link');

    navLinks.forEach(link => {
        link.classList.remove('active');
        
        const href = link.getAttribute('href');
        if (href === path || (href !== '/' && path.startsWith(href))) {
            link.classList.add('active');
        }
    });
}

function initTerminalCursor() {
    const inputs = document.querySelectorAll('input[type="text"], input[type="password"], textarea');
    inputs.forEach(input => {
        input.addEventListener('focus', function() {
            this.style.caretColor = 'var(--color-cyan)';
        });
    });
}

function initTableRowAnimations() {
    const rows = document.querySelectorAll('tbody tr');
    rows.forEach((row, index) => {
        row.style.opacity = '0';
        row.style.transform = 'translateX(-20px)';
        setTimeout(() => {
            row.style.transition = 'all 0.3s ease';
            row.style.opacity = '1';
            row.style.transform = 'translateX(0)';
        }, index * 50);
    });
}

function initGlitchEffect() {
    const glitchElements = document.querySelectorAll('.glitch');
    glitchElements.forEach(el => {
        if (!el.hasAttribute('data-text')) {
            el.setAttribute('data-text', el.textContent);
        }
    });
}

document.addEventListener('DOMContentLoaded', function() {
    highlightActiveNav();
    startConnectionChecker();
    initTerminalCursor();
    initGlitchEffect();
    
    const existingRows = document.querySelectorAll('tbody tr');
    if (existingRows.length > 0 && existingRows.length < 20) {
        initTableRowAnimations();
    }
    
    document.body.style.opacity = '0';
    setTimeout(() => {
        document.body.style.transition = 'opacity 0.5s ease';
        document.body.style.opacity = '1';
    }, 100);
});

window.addEventListener('beforeunload', function() {
    if (connectionCheckInterval) {
        clearInterval(connectionCheckInterval);
    }
});

document.addEventListener('keydown', function(e) {
    if (e.ctrlKey && e.key === 'k') {
        e.preventDefault();
        const searchInput = document.querySelector('input[type="text"], input[type="search"]');
        if (searchInput) {
            searchInput.focus();
        }
    }
});
