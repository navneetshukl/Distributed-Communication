// Chat Client JavaScript
class ChatClient {
    constructor() {
        this.ws = null;
        this.username = '';
        this.currentUserNodeInfo = null; // Store current user's node info
        this.currentRecipient = null;
        this.users = new Map(); // userId -> userInfo (node_id, client_address, etc.)
        this.reconnectAttempts = 0;
        this.maxReconnectAttempts = 5;
        this.msgId = 0;
        this.userRefreshInterval = null;
        this.pendingMessages = new Map(); // msgId -> message content for optimistic UI

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
        this.currentUserInfo = document.getElementById('current-user-info');
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
            
            // Store current user's node info
            this.currentUserNodeInfo = {
                node_id: data.node_id || 'Unknown',
                address: data.address,
                client_address: data.client_address || data.address
            };
            
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
        // Display current user info
        this.displayCurrentUserInfo();
        // Fetch online users after connecting
        this.fetchUsers();
        // Periodically refresh user list
        this.startUserRefreshInterval();
    }

    displayCurrentUserInfo() {
        if (this.currentUserInfo && this.currentUserNodeInfo) {
            this.currentUserInfo.innerHTML = `
                <div class="current-user-name">${this.username}</div>
                <div class="current-user-node">
                    <span class="node-badge">${this.currentUserNodeInfo.node_id}</span>
                    <span class="node-address">${this.currentUserNodeInfo.client_address}</span>
                </div>
            `;
        }
    }

    startUserRefreshInterval() {
        if (this.userRefreshInterval) {
            clearInterval(this.userRefreshInterval);
        }
        this.userRefreshInterval = setInterval(() => this.fetchUsers(), 1000); // Every 30 seconds
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
        // Clear current user info
        if (this.currentUserInfo) {
            this.currentUserInfo.innerHTML = '';
        }
        this.currentUserNodeInfo = null;

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
                // Get current online users from registry
                const onlineUsers = new Set();
                Object.keys(data.users).forEach(userId => {
                    if (userId !== this.username) {
                        onlineUsers.add(userId);
                        const userInfo = data.users[userId];
                        this.updateUserInList(userId, userInfo);
                    }
                });

                // Remove users who are no longer online
                for (const userId of this.users.keys()) {
                    if (!onlineUsers.has(userId)) {
                        this.removeUserFromList(userId);
                    }
                }
            }
        } catch (err) {
            console.error('Failed to fetch users:', err);
        }
    }

    updateUserInList(userId, userInfo) {
        // Update stored user info
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

        // Check if user item already exists
        let item = this.userList.querySelector(`.user-item[data-user="${userId}"]`);
        if (item) {
            // Update existing item
            item.querySelector('.user-details').innerHTML = `
                <div class="user-name">${userId}</div>
                <div class="user-status">Online</div>
                ${nodeDisplay}
            `;
        } else {
            // Create new item
            item = document.createElement('div');
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
    }

    removeUserFromList(userId) {
        this.users.delete(userId);
        const item = this.userList.querySelector(`.user-item[data-user="${userId}"]`);
        if (item) {
            item.remove();
        }
        // If we were chatting with this user, clear the chat
        if (this.currentRecipient === userId) {
            this.currentRecipient = null;
            this.chatHeader.textContent = 'Select a user to chat';
            this.enableInput(false);
            this.messages.innerHTML = '';
        }
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
                // Message acknowledged by server - remove from pending and confirm
                this.onMessageAcknowledged(msg.msg_id);
                break;
            case 'error':
                this.displayError(msg.error);
                break;
            case 'pong':
                break;
        }
    }

    displayMessage(msg) {

        console.log("Display Message is ",msg)

        const isSent = msg.from === this.username;
        const isSystem = msg.from === 'system';

        console.log("isSent ",isSent)
        console.log("userName ",this.username)
        console.log("From ",msg.from)
        console.log("Current Receipient ",this.currentRecipient)
        this.currentRecipient=msg.from

        // If message from another user not in list, add them
        if (msg.from !== this.username && msg.from !== this.currentRecipient) {
            this.updateUserInList(msg.from, null);
        }

        // Display message if it's for the current conversation
        if (this.currentRecipient === msg.from || (isSent && this.currentRecipient === msg.to)) {
            console.log("Inside the current receipient")
            this.addMessageToChat(msg, isSent, isSystem);
        } else if (!isSent) {
            // Notification for message from another user (could add badge later)
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
            // Store msg_id for potential acknowledgment handling
            if (msg.msg_id) {
                div.dataset.msgId = msg.msg_id;
            }
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

        const msgId = `msg_${++this.msgId}_${Date.now()}`;
        const msg = {
            type: 'chat',
            to: this.currentRecipient,
            content: content,
            msg_id: msgId
        };

        if (this.ws && this.ws.readyState === WebSocket.OPEN) {
            // Optimistic UI: display message immediately before server confirmation
            const optimisticMsg = {
                ...msg,
                from: this.username,
                timestamp: Date.now()
            };
            this.addMessageToChat(optimisticMsg, true);
            this.messageInput.value = '';

            // Store for potential acknowledgment handling
            this.pendingMessages.set(msgId, optimisticMsg);

            this.ws.send(JSON.stringify(msg));
        }
    }

    onMessageAcknowledged(msgId) {
        // Message was acknowledged by server
        // Could add visual confirmation (checkmark, etc.) here
        this.pendingMessages.delete(msgId);
        console.log('Message acknowledged:', msgId);
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