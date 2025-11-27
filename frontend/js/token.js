// Token page functionality

class TokenPage {
    constructor() {
        this.auth = new GitHubAuth();
    }

    async init() {
        await this.auth.loadConfig();
        await this.checkAuthAndLoadToken();
    }

    async checkAuthAndLoadToken() {
        const loading = document.getElementById('loading');
        const tokenContent = document.getElementById('token-content');
        const unauthorized = document.getElementById('unauthorized');

        if (!this.auth.isLoggedIn()) {
            // Redirect to home if not logged in
            window.location.href = '/';
            return;
        }

        try {
            // Fetch user data
            const response = await fetch('/api/user', {
                headers: {
                    'Authorization': `Bearer ${this.auth.accessToken}`
                }
            });

            if (!response.ok) {
                throw new Error('Failed to fetch user data');
            }

            const user = await response.json();
            
            loading.classList.add('hidden');

            if (user.isPremium && user.apiToken) {
                this.showTokenPage(user);
            } else {
                this.showUnauthorized();
            }
        } catch (error) {
            console.error('Error:', error);
            loading.classList.add('hidden');
            this.showUnauthorized();
        }
    }

    showTokenPage(user) {
        const tokenContent = document.getElementById('token-content');
        const userInfo = document.getElementById('user-info');
        const apiTokenInput = document.getElementById('api-token');
        const expiresDate = document.getElementById('expires-date');
        const copyBtn = document.getElementById('copy-btn');
        const revokeBtn = document.getElementById('revoke-btn');

        // Store current user data
        this.currentUser = user;

        // Show user info
        userInfo.textContent = `Welcome, ${user.login}`;

        // Set token value
        apiTokenInput.value = user.apiToken;

        // Set expiration date
        const expires = new Date(user.expiresAt);
        expiresDate.textContent = expires.toLocaleDateString('en-US', {
            year: 'numeric',
            month: 'long',
            day: 'numeric'
        });

        // Set up copy functionality
        copyBtn.addEventListener('click', () => {
            navigator.clipboard.writeText(this.currentUser.apiToken);
            copyBtn.textContent = 'Copied!';
            copyBtn.classList.add('bg-green-500');
            copyBtn.classList.remove('bg-primary');
            
            setTimeout(() => {
                copyBtn.textContent = 'Copy';
                copyBtn.classList.remove('bg-green-500');
                copyBtn.classList.add('bg-primary');
            }, 2000);
        });

        // Set up revoke functionality
        revokeBtn.addEventListener('click', () => {
            this.handleRevokeToken();
        });

        tokenContent.classList.remove('hidden');
    }

    async handleRevokeToken() {
        const revokeBtn = document.getElementById('revoke-btn');
        
        // Confirm action
        if (!confirm('Are you sure you want to revoke your current token? This will immediately invalidate the current token and generate a new one.')) {
            return;
        }

        // Set loading state
        revokeBtn.disabled = true;
        revokeBtn.textContent = 'Revoking...';
        revokeBtn.classList.add('opacity-50');

        try {
            const response = await fetch('/api/revoke-token', {
                method: 'POST',
                headers: {
                    'Authorization': `Bearer ${this.auth.accessToken}`,
                    'Content-Type': 'application/json'
                }
            });

            if (!response.ok) {
                throw new Error('Failed to revoke token');
            }

            const result = await response.json();
            
            // Update the displayed token
            const apiTokenInput = document.getElementById('api-token');
            apiTokenInput.value = result.apiToken;
            this.currentUser.apiToken = result.apiToken;

            // Show success message
            this.showNotification('Token revoked successfully! A new token has been generated.', 'success');

        } catch (error) {
            console.error('Revoke error:', error);
            this.showNotification('Failed to revoke token. Please try again.', 'error');
        } finally {
            // Reset button state
            revokeBtn.disabled = false;
            revokeBtn.textContent = 'Revoke Token';
            revokeBtn.classList.remove('opacity-50');
        }
    }

    showNotification(message, type) {
        // Create notification element
        const notification = document.createElement('div');
        notification.className = `fixed top-4 right-4 z-50 p-4 rounded-lg text-white max-w-sm ${
            type === 'success' ? 'bg-green-500' : 'bg-red-500'
        }`;
        notification.textContent = message;
        
        document.body.appendChild(notification);

        // Remove after 5 seconds
        setTimeout(() => {
            notification.remove();
        }, 5000);
    }

    showUnauthorized() {
        const unauthorized = document.getElementById('unauthorized');
        unauthorized.classList.remove('hidden');
    }
}

// Initialize when DOM is loaded
document.addEventListener('DOMContentLoaded', () => {
    const tokenPage = new TokenPage();
    tokenPage.init();
});