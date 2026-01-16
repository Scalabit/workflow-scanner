// Main application logic

class remediatorApp {
    constructor() {
        this.auth = new GitHubAuth();
        this.gitlabAuth = new GitLabAuth();
        this.feedback = new FeedbackManager();
        this.loginBtn = null;
    }

    getAccessToken() {
        if (this.gitlabAuth && this.gitlabAuth.isLoggedIn()) return this.gitlabAuth.accessToken;
        if (this.auth && this.auth.isLoggedIn()) return this.auth.accessToken;
        return localStorage.getItem('accessToken') || null;
    }

    async init() {
        // Load configuration
        await this.auth.loadConfig();
        await this.gitlabAuth.loadConfig();
        
        // Initialize feedback manager
        this.feedback.init();
        
        // Get DOM elements
        this.githubBtn = document.getElementById('github-login-btn');
        this.gitlabBtn = document.getElementById('gitlab-login-btn');
        this.logoutBtn = document.getElementById('logout-btn');
        // legacy
        this.loginBtn = this.githubBtn;
        
        // Check for OAuth callback
        this.handleOAuthCallback();
        
        // Update UI based on auth status (after OAuth processing)
        this.updateUI();

        // Set up event listeners
        this.setupEventListeners();

        setInterval(() => {
            if (this.auth.isLoggedIn() || this.gitlabAuth.isLoggedIn()) {
                this.refreshPremiumStatus();
            }
        }, 3000);
    }

    handleOAuthCallback() {
        const urlParams = new URLSearchParams(window.location.search);
        const hashParams = new URLSearchParams(window.location.hash.substring(1));
        const code = urlParams.get('code');
        const state = urlParams.get('state');
        const accessToken = hashParams.get('access_token');
        const providerFromHash = hashParams.get('provider');

        if (accessToken) {
            // Handle direct auth data from OAuth callback redirect (from hash)
            const userLogin = hashParams.get('user_login');
            const userId = hashParams.get('user_id');
            const userEmail = hashParams.get('user_email');

            if (userLogin && userId && userEmail) {
                const user = {
                    login: userLogin,
                    id: parseInt(userId),
                    email: userEmail
                };

                if (providerFromHash === 'gitlab') {
                    this.gitlabAuth.storeAuthData(accessToken, user);
                    this.showLoggedInState(user.login);
                } else {
                    this.auth.storeAuthData(accessToken, user);
                    this.showLoggedInState(user.login);
                }

                // Clear URL hash
                window.history.replaceState({}, document.title, window.location.pathname);
            }
        } else if (code) {
            if (state === 'gitlab') {
                this.processAuthCallback(code, 'gitlab');
            } else {
                this.processAuthCallback(code, 'github');
            }
        }
    }

    async processAuthCallback(code, provider = 'github') {
        try {
            this.showLoadingState();
            let user;
            if (provider === 'gitlab') {
                user = await this.gitlabAuth.handleCallback(code);
            } else {
                user = await this.auth.handleCallback(code);
            }

            this.showLoggedInState(user.login);

            // Clear URL parameters
            window.history.replaceState({}, document.title, window.location.pathname);
        } catch (error) {
            this.showError('Authentication failed. Please try again.');
            console.error('Auth callback error:', error);
        }
    }

    setupEventListeners() {
        if (this.githubBtn) {
            this.githubBtn.addEventListener('click', (e) => {
                e.preventDefault();
                this.handleLoginClick();
            });
        }

        if (this.gitlabBtn) {
            this.gitlabBtn.addEventListener('click', (e) => {
                e.preventDefault();
                this.gitlabAuth.initiateLogin();
            });
        }

        if (this.logoutBtn) {
            this.logoutBtn.addEventListener('click', (e) => {
                e.preventDefault();
                this.logout();
            });
        }
    }

    handleLoginClick() {
        if (this.auth.isLoggedIn() || this.gitlabAuth.isLoggedIn()) {
            this.logout();
        } else {
            this.login();
        }
    }

    login() {
        this.auth.initiateLogin();
    }

    logout() {
        if (this.auth && this.auth.isLoggedIn()) {
            this.auth.logout();
        }
        if (this.gitlabAuth && this.gitlabAuth.isLoggedIn()) {
            this.gitlabAuth.logout();
        }
        this.showLoggedOutState();
    }

    updateUI() {
        if (this.gitlabAuth.isLoggedIn()) {
            this.showLoggedInState(this.gitlabAuth.userName);
        } else if (this.auth.isLoggedIn()) {
            this.showLoggedInState(this.auth.userName);
        } else {
            this.showLoggedOutState();
        }
    }

    refreshPremiumStatus() {
        if (this.auth.isLoggedIn() || this.gitlabAuth.isLoggedIn()) {
            this.checkPremiumStatus();
        }
    }

    showLoggedInState(userName) {
        if (this.githubBtn) this.githubBtn.classList.add('hidden');
        if (this.gitlabBtn) this.gitlabBtn.classList.add('hidden');

        if (this.logoutBtn) {
            this.logoutBtn.textContent = `Logout (${userName})`;
            this.logoutBtn.classList.remove('hidden');
        }

        this.checkPremiumStatus();
    }

    async checkPremiumStatus() {
        try {
            const token = this.getAccessToken();
            if (!token) {
                this.showLoggedOutState();
                return;
            }

            const response = await fetch('/api/user', {
                headers: {
                    'Authorization': `Bearer ${token}`,
                    'Accept': 'application/json'
                }
            });

            if (response.ok) {
                const text = await response.text();
                if (!text) {
                    this.showPaymentSection();
                    return;
                }
                let user;
                try {
                    user = JSON.parse(text);
                } catch (err) {
                    console.error('Failed parsing /api/user response:', err);
                    this.showPaymentSection();
                    return;
                }

                if (user.isPremium) {
                    this.showPremiumStatus(user);
                } else {
                    this.showPaymentSection();
                }
            } else {
                this.showPaymentSection();
            }
        } catch (error) {
            console.error('Failed to check premium status:', error);
            if (this.getAccessToken()) {
                this.showPaymentSection();
            } else {
                this.showLoggedOutState();
            }
        }
    }

    showPremiumStatus(user) {
        const paymentSection = document.getElementById('payment-section');
        if (paymentSection) {
            const maskedToken = this.maskToken(user.apiToken);
            
            paymentSection.innerHTML = `
                <div class="bg-green-50 border border-green-200 rounded-lg p-4">
                    <p class="text-green-600 font-bold text-lg mb-3">✓ Premium User</p>
                    <a href="/token" 
                       class="inline-block bg-blue-500 hover:bg-blue-600 text-white px-4 py-2 rounded-lg text-sm font-medium">
                        View API Token
                    </a>
                    <p class="text-xs text-gray-500 mt-2">Token: ${maskedToken}</p>
                </div>
            `;
            paymentSection.classList.remove('hidden');
        }
    }

    maskToken(token) {
        if (token.length < 8) return token;
        const start = token.substring(0, 5);
        const end = token.substring(token.length - 3);
        return `${start}***${end}`;
    }


    showPaymentSection() {
        const paymentSection = document.getElementById('payment-section');
        if (paymentSection) {
            paymentSection.classList.remove('hidden');
            this.setupPayment();
        }
    }

    showLoggedOutState() {
        if (this.githubBtn) {
            this.githubBtn.textContent = 'Login with GitHub';
            this.githubBtn.classList.remove('hidden');
            this.githubBtn.classList.remove('bg-green-500', 'text-white', 'bg-blue-500', 'bg-red-500');
            this.githubBtn.classList.add('bg-white', 'text-primarydark');
        }

        if (this.gitlabBtn) {
            this.gitlabBtn.classList.remove('hidden');
        }

        if (this.logoutBtn) {
            this.logoutBtn.classList.add('hidden');
        }

        // Hide payment section when logged out
        const paymentSection = document.getElementById('payment-section');
        if (paymentSection) {
            paymentSection.classList.add('hidden');
        }
    }

    setupPayment() {
        const upgradeBtn = document.getElementById('upgrade-btn');
        if (upgradeBtn) {
            upgradeBtn.addEventListener('click', () => {
                this.handleUpgrade();
            });
        }
    }

    async handleUpgrade() {
        if (!this.getAccessToken()) {
            alert('Please login first');
            return;
        }

        try {
            this.setUpgradeButtonLoading(true);

            // Create checkout session
            const response = await fetch('/api/create-checkout-session', {
                method: 'POST',
                headers: {
                    'Authorization': `Bearer ${this.getAccessToken()}`,
                    'Content-Type': 'application/json'
                }
            });

            if (!response.ok) {
                const errText = await response.text();
                throw new Error('Failed to create checkout session: ' + (errText || response.statusText));
            }

            const text = await response.text();
            if (!text) {
                throw new Error('Empty response from server when creating checkout session');
            }
            let data;
            try {
                data = JSON.parse(text);
            } catch (err) {
                throw new Error('Invalid JSON from server when creating checkout session: ' + err.message);
            }

            const checkout_url = data.checkout_url;
            if (!checkout_url) {
                throw new Error('No checkout URL returned from server');
            }

            // Redirect to Stripe Checkout
            window.location.href = checkout_url;

        } catch (error) {
            console.error('Payment error:', error);
            alert('Payment failed: ' + error.message);
        } finally {
            this.setUpgradeButtonLoading(false);
        }
    }

    setUpgradeButtonLoading(loading) {
        const upgradeBtn = document.getElementById('upgrade-btn');
        if (upgradeBtn) {
            if (loading) {
                upgradeBtn.textContent = 'Processing...';
                upgradeBtn.disabled = true;
                upgradeBtn.classList.add('opacity-50', 'cursor-not-allowed');
            } else {
                upgradeBtn.textContent = 'Upgrade to Premium';
                upgradeBtn.disabled = false;
                upgradeBtn.classList.remove('opacity-50', 'cursor-not-allowed');
            }
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
    const app = new remediatorApp();
    app.init();
});