class FeedbackManager {
    constructor() {
        this.modal = null;
        this.form = null;
        this.statusElement = null;
    }

    init() {
        this.modal = document.getElementById('feedback-modal');
        this.form = document.getElementById('feedback-form');
        this.statusElement = document.getElementById('feedback-status');

        const feedbackBtn = document.getElementById('feedback-btn');
        const closeBtn = document.getElementById('close-feedback-modal');
        const cancelBtn = document.getElementById('cancel-feedback');

        if (feedbackBtn) {
            feedbackBtn.addEventListener('click', () => {
                this.openModal();
            });
        }

        if (closeBtn) {
            closeBtn.addEventListener('click', () => {
                this.closeModal();
            });
        }

        if (cancelBtn) {
            cancelBtn.addEventListener('click', () => {
                this.closeModal();
            });
        }

        if (this.modal) {
            this.modal.addEventListener('click', (e) => {
                if (e.target === this.modal) {
                    this.closeModal();
                }
            });
        }

        if (this.form) {
            this.form.addEventListener('submit', (e) => {
                e.preventDefault();
                this.submitFeedback();
            });
        }

        document.addEventListener('keydown', (e) => {
            if (e.key === 'Escape' && this.modal && this.modal.classList.contains('active')) {
                this.closeModal();
            }
        });
    }

    openModal() {
        const userName = localStorage.getItem('userName');
        const userEmail = localStorage.getItem('userEmail');
        
        const nameInput = document.getElementById('feedback-name');
        const emailInput = document.getElementById('feedback-email');
        
        if (userName && nameInput) {
            nameInput.value = userName;
        }
        
        if (userEmail && emailInput) {
            emailInput.value = userEmail;
            emailInput.readOnly = true;
            emailInput.classList.add('bg-gray-100', 'cursor-not-allowed');
        }
        
        this.modal.classList.add('active');
        document.body.style.overflow = 'hidden';
    }

    closeModal() {
        this.modal.classList.remove('active');
        document.body.style.overflow = '';
        
        const emailInput = document.getElementById('feedback-email');
        if (emailInput) {
            emailInput.readOnly = false;
            emailInput.classList.remove('bg-gray-100', 'cursor-not-allowed');
        }
        
        this.form.reset();
        this.hideStatus();
    }

    showStatus(message, isError = false) {
        this.statusElement.textContent = message;
        this.statusElement.className = `text-sm ${isError ? 'text-red-600' : 'text-green-600'}`;
        this.statusElement.classList.remove('hidden');
    }

    hideStatus() {
        this.statusElement.classList.add('hidden');
    }

    async submitFeedback() {
        const name = document.getElementById('feedback-name').value.trim();
        const email = document.getElementById('feedback-email').value.trim();
        const message = document.getElementById('feedback-message').value.trim();

        if (!name || !email || !message) {
            this.showStatus('Please fill in all fields', true);
            return;
        }

        try {
            const submitButton = this.form.querySelector('button[type="submit"]');
            submitButton.disabled = true;
            submitButton.textContent = 'Sending...';

            const response = await fetch('/api/feedback', {
                method: 'POST',
                headers: {
                    'Content-Type': 'application/json',
                },
                body: JSON.stringify({
                    name,
                    email,
                    message,
                }),
            });

            if (!response.ok) {
                throw new Error('Failed to send feedback');
            }

            this.showStatus('Thank you! Your feedback has been sent.');
            
            setTimeout(() => {
                this.closeModal();
            }, 2000);

        } catch (error) {
            console.error('Error sending feedback:', error);
            this.showStatus('Failed to send feedback. Please try again.', true);
        } finally {
            const submitButton = this.form.querySelector('button[type="submit"]');
            if (submitButton) {
                submitButton.disabled = false;
                submitButton.textContent = 'Send Feedback';
            }
        }
    }
}
