// GitHub OAuth Authentication module

class GitHubAuth {
    constructor() {
        this.clientId = '';
        this.accessToken = localStorage.getItem('accessToken');
        this.userName = localStorage.getItem('userName');
        this.userEmail = localStorage.getItem('userEmail');
        this.userId = localStorage.getItem('userID');
    }

    async loadConfig() {
        try {
            const response = await fetch('/api/config');
            const config = await response.json();
            this.clientId = config.client_id;
        } catch (error) {
            console.error('Failed to load config:', error);
        }
    }

    isLoggedIn() {
        return this.accessToken && this.userName;
    }

    initiateLogin() {
        if (!this.clientId) {
            console.error('Client ID not loaded');
            return;
        }
        
        const redirectUri = `${window.location.origin}/`;
        const scope = 'user:email,repo';
        const githubAuthUrl = `https://github.com/login/oauth/authorize?client_id=${this.clientId}&redirect_uri=${encodeURIComponent(redirectUri)}&scope=${scope}`;
        
        console.log('Redirecting to:', githubAuthUrl);
        window.location.href = githubAuthUrl;
    }

    async handleCallback(code) {
        try {
            const response = await fetch('/auth/github', {
                method: 'POST',
                headers: {
                    'Accept': 'application/json',
                    'Content-Type': 'application/json'
                },
                body: JSON.stringify({ code: code })
            });
            
            if (!response.ok) {
                throw new Error('Authentication failed');
            }
            
            const data = await response.json();
            
            if (data.access_token) {
                this.storeAuthData(data.access_token, data.user);
                return data.user;
            } else {
                throw new Error('No access token received');
            }
        } catch (error) {
            console.error('Authentication error:', error);
            throw error;
        }
    }

    storeAuthData(accessToken, user) {
        this.accessToken = accessToken;
        this.userName = user.login;
        this.userEmail = user.email;
        this.userId = user.id;

        localStorage.setItem('accessToken', accessToken);
        localStorage.setItem('userName', user.login);
        localStorage.setItem('userEmail', user.email);
        localStorage.setItem('userID', user.id);
    }

    logout() {
        this.accessToken = null;
        this.userName = null;
        this.userEmail = null;
        this.userId = null;

        localStorage.removeItem('accessToken');
        localStorage.removeItem('userName');
        localStorage.removeItem('userEmail');
        localStorage.removeItem('userID');
    }

    async getUserData() {
        if (!this.accessToken) {
            throw new Error('No access token available');
        }

        try {
            const response = await fetch('/api/user', {
                headers: {
                    'Authorization': `Bearer ${this.accessToken}`,
                    'Accept': 'application/json'
                }
            });

            if (!response.ok) {
                throw new Error('Failed to fetch user data');
            }

            return await response.json();
        } catch (error) {
            console.error('Error fetching user data:', error);
            throw error;
        }
    }
}