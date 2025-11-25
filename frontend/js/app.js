// Main application logic

class flowsnifferApp {
    constructor() {
        this.auth = new GitHubAuth();
        this.loginBtn = null;
    }

    async init() {
        // Load configuration
        await this.auth.loadConfig();
        
        // Get DOM elements
        this.loginBtn = document.getElementById('github-login-btn');
        
        // Check for OAuth callback
        this.handleOAuthCallback();
        
        // Update UI based on auth status
        this.updateUI();
        
        // Set up event listeners
        this.setupEventListeners();
    }

    handleOAuthCallback() {
        const urlParams = new URLSearchParams(window.location.search);
        const code = urlParams.get('code');
        
        if (code) {
            this.processAuthCallback(code);
        }
    }

    async processAuthCallback(code) {
        try {
            this.showLoadingState();
            const user = await this.auth.handleCallback(code);
            this.showLoggedInState(user.login);
            
            // Clear URL parameters
            window.history.replaceState({}, document.title, window.location.pathname);
        } catch (error) {
            this.showError('Authentication failed. Please try again.');
            console.error('Auth callback error:', error);
        }
    }

    setupEventListeners() {
        if (this.loginBtn) {
            this.loginBtn.addEventListener('click', (e) => {
                e.preventDefault();
                this.handleLoginClick();
            });
        }
    }

    handleLoginClick() {
        if (this.auth.isLoggedIn()) {
            this.logout();
        } else {
            this.login();
        }
    }

    login() {
        this.auth.initiateLogin();
    }

    logout() {
        this.auth.logout();
        this.showLoggedOutState();
    }

    updateUI() {
        if (this.auth.isLoggedIn()) {
            this.showLoggedInState(this.auth.userName);
        } else {
            this.showLoggedOutState();
        }
    }

    showLoggedInState(userName) {
        if (this.loginBtn) {
            this.loginBtn.textContent = `Welcome, ${userName}! (Logout)`;
            this.loginBtn.classList.remove('bg-white', 'text-primarydark');
            this.loginBtn.classList.add('bg-green-500', 'text-white');
        }
    }

    showLoggedOutState() {
        if (this.loginBtn) {
            this.loginBtn.textContent = 'Login with GitHub';
            this.loginBtn.classList.remove('bg-green-500', 'text-white', 'bg-blue-500', 'bg-red-500');
            this.loginBtn.classList.add('bg-white', 'text-primarydark');
        }
    }

    showLoadingState() {
        if (this.loginBtn) {
            this.loginBtn.textContent = 'Authenticating...';
            this.loginBtn.classList.remove('bg-white', 'text-primarydark', 'bg-green-500', 'text-white');
            this.loginBtn.classList.add('bg-blue-500', 'text-white');
        }
    }

    showError(message) {
        if (this.loginBtn) {
            this.loginBtn.textContent = 'Login Failed - Try Again';
            this.loginBtn.classList.remove('bg-white', 'text-primarydark', 'bg-green-500', 'bg-blue-500');
            this.loginBtn.classList.add('bg-red-500', 'text-white');
            
            setTimeout(() => {
                this.showLoggedOutState();
            }, 3000);
        }
        
        console.error(message);
    }
}

// Initialize the app when the DOM is loaded
document.addEventListener('DOMContentLoaded', () => {
    const app = new flowsnifferApp();
    app.init();
});