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

function confirmAction(message) {
    return confirm(message);
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
    if (!confirmAction('[CONFIRM] Delete this token? This action cannot be undone.')) {
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
