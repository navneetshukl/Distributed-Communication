// Chat Client JavaScript
class ChatClient {
    constructor() {
        this.ws = null;
        this.username = '';
        this.currentRecipient = null;
        this.users = new Map();
        this.reconnectAttempts = 0;
        this.maxReconnectAttempts = 5;
        this.msgId = 0;

        this.initElements();
        this.bindEvents();
    }

    initElements() {
        this.loginScreen = document.getElementById('login-screen');
        this.chatContainer = document.getElementById('chat-container');
        this.usernameInput = document.getElementById('username-input');
        this.nodeUrlInput = document.getElementById('node-url-input');
        this.registryUrl = 'http://localhost:8080';
        this.loginBtn = document.getElementById('login-btn');
        this.userList = document.getElementById('user-list');
        this.chatHeader = document.getElementById('chat-header');
        this.messages = document.getElementById('messages');
        this.messageInput = document.getElementById('message-input');
        this.sendBtn = document.getElementById('send-btn');
        this.statusDot = document.getElementById('status-dot');
        this.statusText = document.getElementById('status-text');
        this.refreshUsersBtn = document.getElementById('refresh-users-btn');
    }

    bindEvents() {
        this.loginBtn.addEventListener('click', () => this.connect());
        this.usernameInput.addEventListener('keypress', (e) => {
            if (e.key === 'Enter') this.connect();
        });
        this.sendBtn.addEventListener('click', () => this.sendMessage());
        this.messageInput.addEventListener('keypress', (e) => {
            if (e.key === 'Enter') this.sendMessage();
        });
        if (this.refreshUsersBtn) {
            this.refreshUsersBtn.addEventListener('click', () => this.fetchUsers());
        }
    }
async connect() {
        this.username = this.usernameInput.value.trim();

        if (!this.username) {
            alert('Please enter a username');
            return;
        }

        this.loginBtn.disabled = true;
        this.loginBtn.textContent = 'Finding node...';

        // Get node assignment from registry (round-robin)
        try {
            const response = await fetch(`${this.registryUrl}/assign?user_id=${encodeURIComponent(this.username)}`);
            if (!response.ok) {
                throw new Error('No nodes available');
            }
            const data = await response.json();
            const nodeUrl = data.address.replace('http://', 'ws://').replace('https://', 'wss://');
            
            this.loginBtn.textContent = 'Connecting...';
            const wsUrl = `${nodeUrl}/ws?user=${encodeURIComponent(this.username)}`;
            this.ws = new WebSocket(wsUrl);

            this.ws.onopen = () => this.onOpen();
            this.ws.onmessage = (e) => this.onMessage(e);
            this.ws.onclose = () => this.onClose();
            this.ws.onerror = (e) => this.onError(e);
        } catch (err) {
            console.error('Failed to assign node:', err);
            alert('Could not connect to chat service: ' + err.message);
            this.loginBtn.disabled = false;
            this.loginBtn.textContent = 'Connect';
        }
    }

    onOpen() {
        console.log('Connected to server');
        this.setConnectionStatus(true);
        this.loginScreen.style.display = 'none';
        this.chatContainer.style.display = 'flex';
        this.messageInput.disabled = false;
        this.sendBtn.disabled = false;
        this.reconnectAttempts = 0;
        // Fetch online users after connecting
        this.fetchUsers();
        // Periodically refresh user list
        this.startUserRefreshInterval();
    }

    startUserRefreshInterval() {
        if (this.userRefreshInterval) {
            clearInterval(this.userRefreshInterval);
        }
        this.userRefreshInterval = setInterval(() => this.fetchUsers(), 30000); // Every 30 seconds
    }

    stopUserRefreshInterval() {
        if (this.userRefreshInterval) {
            clearInterval(this.userRefreshInterval);
            this.userRefreshInterval = null;
        }
    }

    onClose() {
        console.log('Disconnected from server');
        this.setConnectionStatus(false);
        this.enableInput(false);
        this.stopUserRefreshInterval();

        if (this.reconnectAttempts < this.maxReconnectAttempts) {
            this.reconnectAttempts++;
            setTimeout(() => this.connect(), 2000 * this.reconnectAttempts);
        }
    }

    onError(error) {
        console.error('WebSocket error:', error);
    }

    async fetchUsers() {
        try {
            const response = await fetch(`${this.registryUrl}/users`);
            if (!response.ok) {
                throw new Error('Failed to fetch users');
            }
            const data = await response.json();
            if (data.users) {
                Object.keys(data.users).forEach(userId => {
                    if (userId !== this.username) {
                        const userInfo = data.users[userId];
                        this.updateUserInList(userId, userInfo);
                    }
                });
            }
        } catch (err) {
            console.error('Failed to fetch users:', err);
        }
    }

    updateUserInList(userId, userInfo) {
        if (this.users.has(userId)) return;

        this.users.set(userId, userInfo);

        let nodeDisplay = '';
        if (userInfo && userInfo.node_id) {
            nodeDisplay = `
                <div class="node-info">
                    <span class="node-badge">${userInfo.node_id}</span>
                    <span class="node-address">${userInfo.client_address}</span>
                </div>
            `;
        }

        const item = document.createElement('div');
        item.className = 'user-item';
        item.dataset.user = userId;
        item.innerHTML = `
            <div class="user-avatar">${userId.charAt(0).toUpperCase()}</div>
            <div class="user-details">
                <div class="user-name">${userId}</div>
                <div class="user-status">Online</div>
                ${nodeDisplay}
            </div>
        `;
        item.addEventListener('click', () => this.selectUser(userId));
        this.userList.appendChild(item);
    }

    onMessage(event) {
        try {
            const msg = JSON.parse(event.data);
            this.handleMessage(msg);
        } catch (e) {
            console.error('Failed to parse message:', e);
        }
    }

    handleMessage(msg) {
        switch (msg.type) {
            case 'chat':
                this.displayMessage(msg);
                break;
            case 'ack':
                console.log('Message acknowledged:', msg.msg_id);
                break;
            case 'error':
                this.displayError(msg.error);
                break;
            case 'pong':
                break;
        }
    }

    displayMessage(msg) {
        const isSent = msg.from === this.username;
        const isSystem = msg.from === 'system';

        if (msg.from !== this.username && msg.from !== this.currentRecipient) {
            this.updateUserInList(msg.from, null);
        }

        if (this.currentRecipient === msg.from || (isSent && this.currentRecipient === msg.to)) {
            this.addMessageToChat(msg, isSent, isSystem);
        } else {
            this.updateUserInList(msg.from, null);
        }
    }

    addMessageToChat(msg, isSent, isSystem = false) {
        const div = document.createElement('div');
        if (isSystem) {
            div.className = 'message system';
            div.textContent = msg.content;
        } else {
            div.className = `message ${isSent ? 'sent' : 'received'}`;
            div.textContent = msg.content;
        }
        this.messages.appendChild(div);
        this.messages.scrollTop = this.messages.scrollHeight;
    }

    displayError(error) {
        const div = document.createElement('div');
        div.className = 'message error';
        div.textContent = error;
        this.messages.appendChild(div);
        this.messages.scrollTop = this.messages.scrollHeight;
    }

    sendMessage() {
        const content = this.messageInput.value.trim();
        if (!content || !this.currentRecipient) return;

        const msg = {
            type: 'chat',
            to: this.currentRecipient,
            content: content,
            msg_id: `msg_${++this.msgId}_${Date.now()}`
        };

        if (this.ws && this.ws.readyState === WebSocket.OPEN) {
            this.ws.send(JSON.stringify(msg));
            this.messageInput.value = '';
        }
    }

    selectUser(userId) {
        this.currentRecipient = userId;
        this.chatHeader.textContent = `Chat with ${userId}`;
        this.enableInput(true);
        this.messages.innerHTML = '';

        document.querySelectorAll('.user-item').forEach(item => {
            item.classList.toggle('active', item.dataset.user === userId);
        });
    }

    setConnectionStatus(connected) {
        this.statusDot.className = `status-dot ${connected ? 'status-connected' : 'status-disconnected'}`;
        this.statusText.textContent = connected ? 'Connected' : 'Disconnected';
    }

    enableInput(enabled) {
        this.messageInput.disabled = !enabled;
        this.sendBtn.disabled = !enabled;
    }
}

document.addEventListener('DOMContentLoaded', () => {
    new ChatClient();
});