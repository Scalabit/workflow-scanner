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
        
        // Update UI based on auth status (after OAuth processing)
        this.updateUI();
        
        // Set up event listeners
        this.setupEventListeners();

        setInterval(() => {
            if (this.auth.isLoggedIn()) {
                this.refreshPremiumStatus();
            }
        }, 3000);
    }

    handleOAuthCallback() {
        const urlParams = new URLSearchParams(window.location.search);
        const hashParams = new URLSearchParams(window.location.hash.substring(1));
        const code = urlParams.get('code');
        const accessToken = hashParams.get('access_token');
        
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
                this.auth.storeAuthData(accessToken, user);
                
                // Clear URL hash
                window.history.replaceState({}, document.title, window.location.pathname);
            }
        } else if (code) {
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

    refreshPremiumStatus() {
        if (this.auth.isLoggedIn()) {
            this.checkPremiumStatus();
        }
    }

    showLoggedInState(userName) {
        if (this.loginBtn) {
            this.loginBtn.textContent = `Welcome, ${userName}! (Logout)`;
            this.loginBtn.classList.remove('bg-white', 'text-primarydark');
            this.loginBtn.classList.add('bg-green-500', 'text-white');
        }
        
        this.checkPremiumStatus();
    }

    async checkPremiumStatus() {
        try {
            const response = await fetch('/api/user', {
                headers: {
                    'Authorization': `Bearer ${this.auth.accessToken}`
                }
            });
            
            if (response.ok) {
                const user = await response.json();
                if (user.isPremium) {
                    this.showPremiumStatus(user);
                } else {
                    this.showPaymentSection();
                }
            }
        } catch (error) {
            console.error('Failed to check premium status:', error);
            this.showPaymentSection();
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
        if (this.loginBtn) {
            this.loginBtn.textContent = 'Login with GitHub';
            this.loginBtn.classList.remove('bg-green-500', 'text-white', 'bg-blue-500', 'bg-red-500');
            this.loginBtn.classList.add('bg-white', 'text-primarydark');
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
        if (!this.auth.isLoggedIn()) {
            alert('Please login first');
            return;
        }

        try {
            this.setUpgradeButtonLoading(true);

            // Create checkout session
            const response = await fetch('/api/create-checkout-session', {
                method: 'POST',
                headers: {
                    'Authorization': `Bearer ${this.auth.accessToken}`,
                    'Content-Type': 'application/json'
                }
            });

            if (!response.ok) {
                throw new Error('Failed to create checkout session');
            }

            const { checkout_url } = await response.json();

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
    const app = new flowsnifferApp();
    app.init();
});