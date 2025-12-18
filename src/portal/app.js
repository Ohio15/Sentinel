/**
 * Sentinel Support Portal - Frontend Application
 */

// State

// Theme management
function initTheme() {
  const savedTheme = localStorage.getItem('portal-theme');
  if (savedTheme === 'dark') {
    document.documentElement.classList.add('dark');
    updateThemeIcon(true);
  }
}

function toggleTheme() {
  const isDark = document.documentElement.classList.toggle('dark');
  localStorage.setItem('portal-theme', isDark ? 'dark' : 'light');
  updateThemeIcon(isDark);
}

function updateThemeIcon(isDark) {
  const sunIcon = document.getElementById('sunIcon');
  const moonIcon = document.getElementById('moonIcon');
  if (sunIcon && moonIcon) {
    sunIcon.style.display = isDark ? 'none' : 'block';
    moonIcon.style.display = isDark ? 'block' : 'none';
  }
}

// Initialize theme on load
initTheme();

let currentUser = null;
let clientBranding = null;
let tickets = [];
let currentTicket = null;
let eventSource = null;
let pendingAttachments = [];

// DOM Elements
const loginPage = document.getElementById('loginPage');
const ticketsPage = document.getElementById('ticketsPage');
const newTicketPage = document.getElementById('newTicketPage');
const ticketDetailPage = document.getElementById('ticketDetailPage');
const userInfo = document.getElementById('userInfo');
const userName = document.getElementById('userName');
const errorMessage = document.getElementById('errorMessage');
const ticketsList = document.getElementById('ticketsList');
const ticketDetail = document.getElementById('ticketDetail');
const newTicketForm = document.getElementById('newTicketForm');

// Initialize app on page load
document.addEventListener('DOMContentLoaded', async () => {
  // Check for error in URL params (OAuth callback errors)
  const urlParams = new URLSearchParams(window.location.search);
  const error = urlParams.get('error');
  if (error) {
    showError(decodeURIComponent(error));
    // Clean up URL
    window.history.replaceState({}, document.title, '/portal');
  }

  // Check if user is authenticated
  await checkAuth();
});

/**
 * Check authentication status
 */
async function checkAuth() {
  try {
    const response = await fetch('/portal/auth/me', {
      credentials: 'include'
    });

    if (response.ok) {
      const data = await response.json();
      currentUser = data.user;
      await loadClientBranding();
      showAuthenticatedUI();
      await loadTickets();
    } else {
      showLoginPage();
    }
  } catch (error) {
    console.error('Auth check failed:', error);
    showLoginPage();
  }
}

/**
 * Load client branding and apply it
 */
async function loadClientBranding() {
  try {
    const response = await fetch('/portal/api/client-info', {
      credentials: 'include'
    });

    if (response.ok) {
      clientBranding = await response.json();
      applyBranding();
    }
  } catch (error) {
    console.error('Failed to load client branding:', error);
  }
}

/**
 * Apply client branding to the UI
 */
function applyBranding() {
  if (!clientBranding) return;

  // Apply primary color
  if (clientBranding.primaryColor) {
    document.documentElement.style.setProperty('--primary', clientBranding.primaryColor);
    // Calculate a hover color (slightly darker)
    const hoverColor = adjustColorBrightness(clientBranding.primaryColor, -20);
    document.documentElement.style.setProperty('--primary-hover', hoverColor);
  }

  // Apply logo with configurable size
  const clientLogo = document.getElementById('clientLogo');
  const defaultLogo = document.getElementById('defaultLogo');
  if (clientBranding.logoUrl && clientLogo && defaultLogo) {
    clientLogo.src = clientBranding.logoUrl;
    clientLogo.alt = clientBranding.name || 'Company Logo';
    clientLogo.style.width = (clientBranding.logoWidth || 32) + 'px';
    clientLogo.style.height = (clientBranding.logoHeight || 32) + 'px';
    clientLogo.style.display = 'block';
    defaultLogo.style.display = 'none';
  }

  // Apply company name
  const portalTitle = document.getElementById('portalTitle');
  const welcomeTitle = document.getElementById('welcomeTitle');
  if (clientBranding.name) {
    if (portalTitle) {
      portalTitle.textContent = `${clientBranding.name} Support`;
    }
    if (welcomeTitle) {
      welcomeTitle.textContent = `Welcome to ${clientBranding.name} Support`;
    }
    document.title = `${clientBranding.name} Support Portal`;
  }
}

/**
 * Adjust color brightness for hover states
 */
function adjustColorBrightness(hex, amount) {
  // Remove # if present
  hex = hex.replace(/^#/, '');

  // Parse RGB values
  let r = parseInt(hex.substr(0, 2), 16);
  let g = parseInt(hex.substr(2, 2), 16);
  let b = parseInt(hex.substr(4, 2), 16);

  // Adjust brightness
  r = Math.max(0, Math.min(255, r + amount));
  g = Math.max(0, Math.min(255, g + amount));
  b = Math.max(0, Math.min(255, b + amount));

  // Convert back to hex
  return '#' + [r, g, b].map(x => x.toString(16).padStart(2, '0')).join('');
}

/**
 * Redirect to Microsoft login
 */
function login() {
  window.location.href = '/portal/auth/login';
}

/**
 * Logout user
 */
async function logout() {
  disconnectSSE();

  try {
    await fetch('/portal/auth/logout', {
      method: 'POST',
      credentials: 'include'
    });
  } catch (error) {
    console.error('Logout error:', error);
  }

  currentUser = null;
  clientBranding = null;
  // Reset branding to defaults
  document.documentElement.style.setProperty('--primary', '#0078d4');
  document.documentElement.style.setProperty('--primary-hover', '#106ebe');
  showLoginPage();
}

/**
 * Show login page
 */
function showLoginPage() {
  hideAllPages();
  loginPage.style.display = 'block';
  userInfo.style.display = 'none';
}

/**
 * Show authenticated UI
 */
function showAuthenticatedUI() {
  userName.textContent = currentUser?.name || currentUser?.email || 'User';
  userInfo.style.display = 'flex';
  showTicketsList();
}

/**
 * Show error message
 */
function showError(message) {
  errorMessage.textContent = message;
  errorMessage.style.display = 'block';
}

/**
 * Hide error message
 */
function hideError() {
  errorMessage.style.display = 'none';
}

/**
 * Hide all pages
 */
function hideAllPages() {
  loginPage.style.display = 'none';
  ticketsPage.style.display = 'none';
  newTicketPage.style.display = 'none';
  ticketDetailPage.style.display = 'none';
  disconnectSSE();
}

/**
 * Show tickets list page
 */
function showTicketsList() {
  hideAllPages();
  ticketsPage.style.display = 'block';
  loadTickets();
}

/**
 * Show new ticket form
 */
function showNewTicketForm() {
  hideAllPages();
  newTicketPage.style.display = 'block';
  newTicketForm.reset();
  pendingAttachments = [];

  // Auto-populate "Submitted By" field with current user's name
  const submittedByField = document.getElementById('submittedBy');
  if (submittedByField && currentUser) {
    submittedByField.value = currentUser.name || currentUser.email || '';
  }
}

/**
 * Show ticket detail page
 */
async function showTicketDetail(ticketId) {
  hideAllPages();
  ticketDetailPage.style.display = 'block';
  ticketDetail.innerHTML = '<div class="loading">Loading ticket...</div>';

  try {
    const response = await fetch(`/portal/api/tickets/${ticketId}`, {
      credentials: 'include'
    });

    if (!response.ok) {
      throw new Error('Failed to load ticket');
    }

    currentTicket = await response.json();

    // Load attachments
    const attachmentsResponse = await fetch(`/portal/api/tickets/${ticketId}/attachments`, {
      credentials: 'include'
    });
    if (attachmentsResponse.ok) {
      currentTicket.attachments = await attachmentsResponse.json();
    }

    renderTicketDetail();

    // Connect SSE for real-time updates
    connectSSE(ticketId);
  } catch (error) {
    console.error('Failed to load ticket:', error);
    ticketDetail.innerHTML = '<div class="error-message">Failed to load ticket. Please try again.</div>';
  }
}

/**
 * Load user's tickets
 */
async function loadTickets() {
  ticketsList.innerHTML = '<div class="loading">Loading tickets...</div>';

  try {
    const response = await fetch('/portal/api/tickets', {
      credentials: 'include'
    });

    if (!response.ok) {
      throw new Error('Failed to load tickets');
    }

    tickets = await response.json();
    renderTicketsList();
  } catch (error) {
    console.error('Failed to load tickets:', error);
    ticketsList.innerHTML = '<div class="error-message">Failed to load tickets. Please try again.</div>';
  }
}

/**
 * Render tickets list
 */
function renderTicketsList() {
  if (tickets.length === 0) {
    ticketsList.innerHTML = `
      <div class="empty-state">
        <svg width="64" height="64" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5">
          <path d="M9 5H7a2 2 0 00-2 2v12a2 2 0 002 2h10a2 2 0 002-2V7a2 2 0 00-2-2h-2M9 5a2 2 0 002 2h2a2 2 0 002-2M9 5a2 2 0 012-2h2a2 2 0 012 2"/>
        </svg>
        <h3>No tickets yet</h3>
        <p>Create your first support ticket to get help from our team.</p>
        <button class="btn btn-primary" onclick="showNewTicketForm()">
          Create Ticket
        </button>
      </div>
    `;
    return;
  }

  ticketsList.innerHTML = tickets.map(ticket => `
    <div class="ticket-card" onclick="showTicketDetail('${ticket.id}')">
      <div class="ticket-card-header">
        <span class="ticket-id">${ticket.publicId || 'TKT-' + ticket.id.slice(0, 6).toUpperCase()}</span>
        <span class="status-badge status-${(ticket.status || 'open').toLowerCase().replace(' ', '-')}">${formatStatus(ticket.status)}</span>
      </div>
      <div class="ticket-subject">${escapeHtml(ticket.subject)}</div>
      <div class="ticket-description">${escapeHtml(ticket.description || '')}</div>
      <div class="ticket-meta">
        <span class="priority-badge priority-${(ticket.priority || 'medium').toLowerCase()}">${ticket.priority || 'Medium'}</span>
        <span>Created ${formatDate(ticket.createdAt)}</span>
      </div>
    </div>
  `).join('');
}

/**
 * Render ticket detail
 */
function renderTicketDetail() {
  const ticket = currentTicket;
  const ticketTitle = document.getElementById('ticketDetailTitle');
  ticketTitle.textContent = ticket.publicId || 'TKT-' + ticket.id.slice(0, 6).toUpperCase();

  // Build info items for submitter and device
  let infoItems = '';
  if (ticket.submitterName || ticket.submitterEmail) {
    infoItems += `<span><strong>Submitted by:</strong> ${escapeHtml(ticket.submitterName || ticket.submitterEmail)}</span>`;
  }
  if (ticket.userDeviceName) {
    infoItems += `<span><strong>Device:</strong> ${escapeHtml(ticket.userDeviceName)}</span>`;
  }

  ticketDetail.innerHTML = `
    <div class="ticket-detail-card">
      <div class="ticket-detail-header">
        <div class="ticket-id-large">${ticket.publicId || 'TKT-' + ticket.id.slice(0, 6).toUpperCase()}</div>
        <div class="ticket-detail-subject">${escapeHtml(ticket.subject)}</div>
        <div class="ticket-detail-meta">
          <span class="status-badge status-${(ticket.status || 'open').toLowerCase().replace(' ', '-')}">${formatStatus(ticket.status)}</span>
          <span class="priority-badge priority-${(ticket.priority || 'medium').toLowerCase()}">${ticket.priority || 'Medium'}</span>
          <span>Created ${formatDate(ticket.createdAt)}</span>
          ${ticket.updatedAt ? `<span>Updated ${formatDate(ticket.updatedAt)}</span>` : ''}
        </div>
        ${infoItems ? `<div class="ticket-detail-info">${infoItems}</div>` : ''}
      </div>
      <div class="ticket-detail-body">
        <div class="ticket-detail-description">${escapeHtml(ticket.description || '')}</div>
      </div>
    </div>

    <div class="comments-section">
      <h3>Conversation</h3>
      <div class="comments-container" id="commentsContainer">
        ${renderComments(ticket.comments || [])}
      </div>
      ${ticket.status !== 'closed' ? `
        <div class="comment-form-container">
          <div class="attachment-previews" id="attachmentPreviews"></div>
          <div class="comment-input-row">
            <input type="file" id="fileInput" accept="image/*" multiple style="display: none" onchange="handleFileSelect(event)">
            <button type="button" class="btn-attach" onclick="document.getElementById('fileInput').click()" title="Attach screenshot">
              <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                <path d="M21.44 11.05l-9.19 9.19a6 6 0 0 1-8.49-8.49l9.19-9.19a4 4 0 0 1 5.66 5.66l-9.2 9.19a2 2 0 0 1-2.83-2.83l8.49-8.48"/>
              </svg>
            </button>
            <textarea id="commentContent" placeholder="Type your message..." rows="1" onkeydown="handleCommentKeydown(event)"></textarea>
            <button type="button" class="btn-send" onclick="submitComment()" title="Send message">
              <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                <line x1="22" y1="2" x2="11" y2="13"></line>
                <polygon points="22 2 15 22 11 13 2 9 22 2"></polygon>
              </svg>
            </button>
          </div>
        </div>
      ` : ''}
    </div>
  `;

  // Auto-resize textarea
  const textarea = document.getElementById('commentContent');
  if (textarea) {
    textarea.addEventListener('input', autoResizeTextarea);
  }

  // Scroll to bottom of comments
  scrollToBottomOfComments();
}

/**
 * Render comments with IM-style bubbles and collapsible older comments
 */
function renderComments(comments) {
  if (comments.length === 0) {
    return '<p class="no-comments">No messages yet. Start the conversation!</p>';
  }

  const userEmail = currentUser?.email?.toLowerCase();
  const visibleCount = 2;
  const olderComments = comments.slice(0, Math.max(0, comments.length - visibleCount));
  const recentComments = comments.slice(-visibleCount);

  let html = '';

  // Collapsible older comments section
  if (olderComments.length > 0) {
    html += `
      <div class="older-comments-toggle" id="olderCommentsToggle" onclick="toggleOlderComments()">
        <span id="toggleText">Show ${olderComments.length} older message${olderComments.length > 1 ? 's' : ''}</span>
        <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" id="toggleIcon">
          <polyline points="6 9 12 15 18 9"></polyline>
        </svg>
      </div>
      <div class="older-comments" id="olderComments" style="display: none;">
        ${olderComments.map(comment => renderCommentBubble(comment, userEmail)).join('')}
      </div>
    `;
  }

  // Recent comments (always visible)
  html += recentComments.map(comment => renderCommentBubble(comment, userEmail)).join('');

  return html;
}

/**
 * Render a single comment as IM-style bubble
 */
function renderCommentBubble(comment, userEmail) {
  const isOwnComment = comment.authorEmail?.toLowerCase() === userEmail;
  const bubbleClass = isOwnComment ? 'comment-bubble-own' : 'comment-bubble-other';
  const canEdit = isOwnComment && isWithinEditWindow(comment.createdAt);
  const attachments = getCommentAttachments(comment.id);

  return `
    <div class="comment-bubble ${bubbleClass}" data-comment-id="${comment.id}">
      <div class="comment-bubble-header">
        <span class="comment-author">${escapeHtml(comment.authorName || 'Support')}</span>
        <span class="comment-time">${formatDate(comment.createdAt)}${comment.editedAt ? ' (edited)' : ''}</span>
        ${canEdit ? `
          <button class="btn-edit-comment" onclick="startEditComment('${comment.id}')" title="Edit message">
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <path d="M11 4H4a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-7"/>
              <path d="M18.5 2.5a2.121 2.121 0 0 1 3 3L12 15l-4 1 1-4 9.5-9.5z"/>
            </svg>
          </button>
        ` : ''}
      </div>
      <div class="comment-content" id="comment-content-${comment.id}">${escapeHtml(comment.content)}</div>
      ${attachments.length > 0 ? `
        <div class="comment-attachments">
          ${attachments.map(att => `
            <div class="attachment-thumb" onclick="openLightbox('${att.id}')">
              <img src="/portal/api/attachments/${att.id}?thumbnail=true" alt="${escapeHtml(att.originalFilename)}">
            </div>
          `).join('')}
        </div>
      ` : ''}
    </div>
  `;
}

/**
 * Get attachments for a specific comment
 */
function getCommentAttachments(commentId) {
  if (!currentTicket?.attachments) return [];
  return currentTicket.attachments.filter(att => att.commentId === commentId);
}

/**
 * Check if comment is within 15-minute edit window
 */
function isWithinEditWindow(createdAt) {
  const created = new Date(createdAt);
  const now = new Date();
  const fifteenMinutes = 15 * 60 * 1000;
  return (now.getTime() - created.getTime()) < fifteenMinutes;
}

/**
 * Toggle older comments visibility
 */
function toggleOlderComments() {
  const olderComments = document.getElementById('olderComments');
  const toggleText = document.getElementById('toggleText');
  const toggleIcon = document.getElementById('toggleIcon');

  if (olderComments.style.display === 'none') {
    olderComments.style.display = 'block';
    toggleText.textContent = 'Hide older messages';
    toggleIcon.style.transform = 'rotate(180deg)';
  } else {
    olderComments.style.display = 'none';
    const count = olderComments.querySelectorAll('.comment-bubble').length;
    toggleText.textContent = `Show ${count} older message${count > 1 ? 's' : ''}`;
    toggleIcon.style.transform = 'rotate(0deg)';
  }
}

/**
 * Start editing a comment
 */
function startEditComment(commentId) {
  const contentEl = document.getElementById(`comment-content-${commentId}`);
  if (!contentEl) return;

  const currentContent = contentEl.textContent;
  contentEl.innerHTML = `
    <textarea class="edit-comment-textarea" id="edit-textarea-${commentId}">${escapeHtml(currentContent)}</textarea>
    <div class="edit-comment-actions">
      <button class="btn btn-secondary btn-sm" onclick="cancelEditComment('${commentId}', '${escapeHtml(currentContent).replace(/'/g, "\\'")}')">Cancel</button>
      <button class="btn btn-primary btn-sm" onclick="saveEditComment('${commentId}')">Save</button>
    </div>
  `;

  // Focus and select the textarea
  const textarea = document.getElementById(`edit-textarea-${commentId}`);
  if (textarea) {
    textarea.focus();
    textarea.select();
  }
}

/**
 * Cancel editing a comment
 */
function cancelEditComment(commentId, originalContent) {
  const contentEl = document.getElementById(`comment-content-${commentId}`);
  if (contentEl) {
    contentEl.textContent = originalContent;
  }
}

/**
 * Save edited comment
 */
async function saveEditComment(commentId) {
  const textarea = document.getElementById(`edit-textarea-${commentId}`);
  if (!textarea) return;

  const newContent = textarea.value.trim();
  if (!newContent) {
    alert('Comment cannot be empty');
    return;
  }

  try {
    const response = await fetch(`/portal/api/tickets/${currentTicket.id}/comments/${commentId}`, {
      method: 'PATCH',
      headers: {
        'Content-Type': 'application/json'
      },
      credentials: 'include',
      body: JSON.stringify({ content: newContent })
    });

    if (!response.ok) {
      const error = await response.json();
      throw new Error(error.error || 'Failed to edit comment');
    }

    // Update will come via SSE, but also update locally for immediate feedback
    const contentEl = document.getElementById(`comment-content-${commentId}`);
    if (contentEl) {
      contentEl.textContent = newContent;
    }
  } catch (error) {
    console.error('Failed to edit comment:', error);
    alert(error.message || 'Failed to edit comment. Please try again.');
  }
}

/**
 * Handle file selection for attachments
 */
function handleFileSelect(event) {
  const files = Array.from(event.target.files);
  const previewsContainer = document.getElementById('attachmentPreviews');

  files.forEach(file => {
    if (!file.type.startsWith('image/')) {
      alert('Only image files are allowed');
      return;
    }

    if (file.size > 10 * 1024 * 1024) {
      alert('File size must be less than 10MB');
      return;
    }

    pendingAttachments.push(file);

    // Create preview
    const reader = new FileReader();
    reader.onload = (e) => {
      const preview = document.createElement('div');
      preview.className = 'attachment-preview';
      preview.innerHTML = `
        <img src="${e.target.result}" alt="${escapeHtml(file.name)}">
        <button class="remove-attachment" onclick="removeAttachment(${pendingAttachments.length - 1}, this.parentElement)">
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <line x1="18" y1="6" x2="6" y2="18"></line>
            <line x1="6" y1="6" x2="18" y2="18"></line>
          </svg>
        </button>
      `;
      previewsContainer.appendChild(preview);
    };
    reader.readAsDataURL(file);
  });

  // Clear input for re-selection
  event.target.value = '';
}

/**
 * Remove pending attachment
 */
function removeAttachment(index, element) {
  pendingAttachments.splice(index, 1);
  element.remove();
}

/**
 * Handle Enter key in comment textarea
 */
function handleCommentKeydown(event) {
  if (event.key === 'Enter' && !event.shiftKey) {
    event.preventDefault();
    submitComment();
  }
}

/**
 * Auto-resize textarea
 */
function autoResizeTextarea(event) {
  const textarea = event.target;
  textarea.style.height = 'auto';
  textarea.style.height = Math.min(textarea.scrollHeight, 150) + 'px';
}

/**
 * Scroll to bottom of comments
 */
function scrollToBottomOfComments() {
  const container = document.getElementById('commentsContainer');
  if (container) {
    container.scrollTop = container.scrollHeight;
  }
}

/**
 * Open image lightbox
 */
function openLightbox(attachmentId) {
  // Create lightbox if it doesn't exist
  let lightbox = document.getElementById('lightbox');
  if (!lightbox) {
    lightbox = document.createElement('div');
    lightbox.id = 'lightbox';
    lightbox.className = 'lightbox';
    lightbox.innerHTML = `
      <button class="lightbox-close" onclick="closeLightbox()">&times;</button>
      <img id="lightbox-image" src="" alt="">
    `;
    lightbox.onclick = (e) => {
      if (e.target === lightbox) closeLightbox();
    };
    document.body.appendChild(lightbox);
  }

  const img = document.getElementById('lightbox-image');
  img.src = `/portal/api/attachments/${attachmentId}`;
  lightbox.style.display = 'flex';
  document.body.style.overflow = 'hidden';
}

/**
 * Close lightbox
 */
function closeLightbox() {
  const lightbox = document.getElementById('lightbox');
  if (lightbox) {
    lightbox.style.display = 'none';
    document.body.style.overflow = '';
  }
}

// =========================================================================
// SSE (Server-Sent Events) for Real-Time Updates
// =========================================================================

/**
 * Connect to SSE for real-time updates
 */
function connectSSE(ticketId) {
  disconnectSSE();

  eventSource = new EventSource(`/portal/api/events?ticketId=${ticketId}`);

  eventSource.addEventListener('connected', (event) => {
    console.log('SSE connected:', JSON.parse(event.data));
  });

  eventSource.addEventListener('comment:added', (event) => {
    const data = JSON.parse(event.data);
    if (data.ticketId === currentTicket?.id) {
      handleNewComment(data.comment);
    }
  });

  eventSource.addEventListener('comment:edited', (event) => {
    const data = JSON.parse(event.data);
    if (data.ticketId === currentTicket?.id) {
      handleCommentEdited(data.comment);
    }
  });

  eventSource.addEventListener('attachment:added', (event) => {
    const data = JSON.parse(event.data);
    if (data.ticketId === currentTicket?.id) {
      handleAttachmentAdded(data.attachments);
    }
  });

  eventSource.addEventListener('heartbeat', () => {
    // Heartbeat received, connection is alive
  });

  eventSource.onerror = (error) => {
    console.error('SSE error:', error);
    // Attempt to reconnect after 5 seconds
    setTimeout(() => {
      if (currentTicket) {
        connectSSE(currentTicket.id);
      }
    }, 5000);
  };
}

/**
 * Disconnect SSE
 */
function disconnectSSE() {
  if (eventSource) {
    eventSource.close();
    eventSource = null;
  }
}

/**
 * Handle new comment from SSE
 */
function handleNewComment(comment) {
  // Add to ticket comments
  if (!currentTicket.comments) currentTicket.comments = [];

  // Check if comment already exists (prevent duplicates)
  const exists = currentTicket.comments.some(c => c.id === comment.id);
  if (!exists) {
    currentTicket.comments.push(comment);

    // Re-render comments section
    const container = document.getElementById('commentsContainer');
    if (container) {
      container.innerHTML = renderComments(currentTicket.comments);
      scrollToBottomOfComments();
    }
  }
}

/**
 * Handle comment edited from SSE
 */
function handleCommentEdited(comment) {
  // Update in ticket comments
  if (currentTicket.comments) {
    const index = currentTicket.comments.findIndex(c => c.id === comment.id);
    if (index !== -1) {
      currentTicket.comments[index] = comment;

      // Update the content element
      const contentEl = document.getElementById(`comment-content-${comment.id}`);
      if (contentEl && !contentEl.querySelector('textarea')) {
        contentEl.textContent = comment.content;
      }

      // Update edited indicator in header
      const bubble = document.querySelector(`[data-comment-id="${comment.id}"]`);
      if (bubble) {
        const timeEl = bubble.querySelector('.comment-time');
        if (timeEl && !timeEl.textContent.includes('(edited)')) {
          timeEl.textContent = timeEl.textContent + ' (edited)';
        }
      }
    }
  }
}

/**
 * Handle attachments added from SSE
 */
function handleAttachmentAdded(attachments) {
  // Add to ticket attachments
  if (!currentTicket.attachments) currentTicket.attachments = [];
  currentTicket.attachments.push(...attachments);

  // Re-render comments to show attachments
  const container = document.getElementById('commentsContainer');
  if (container) {
    container.innerHTML = renderComments(currentTicket.comments || []);
    scrollToBottomOfComments();
  }
}

// =========================================================================
// Form Submissions
// =========================================================================

/**
 * Submit new ticket
 */
async function submitTicket(event) {
  event.preventDefault();

  const submitBtn = document.getElementById('submitBtn');
  submitBtn.disabled = true;
  submitBtn.textContent = 'Submitting...';

  const formData = new FormData(newTicketForm);
  const ticketData = {
    subject: formData.get('subject'),
    description: formData.get('description'),
    priority: formData.get('priority'),
    type: formData.get('type'),
    deviceName: formData.get('deviceName') || null
  };

  try {
    const response = await fetch('/portal/api/tickets', {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json'
      },
      credentials: 'include',
      body: JSON.stringify(ticketData)
    });

    if (!response.ok) {
      const error = await response.json();
      throw new Error(error.error || 'Failed to create ticket');
    }

    const newTicket = await response.json();

    // Show success and redirect to ticket detail
    showTicketDetail(newTicket.id);
  } catch (error) {
    console.error('Failed to create ticket:', error);
    alert(error.message || 'Failed to create ticket. Please try again.');
  } finally {
    submitBtn.disabled = false;
    submitBtn.textContent = 'Submit Ticket';
  }
}

/**
 * Submit comment with attachments
 */
async function submitComment() {
  const textarea = document.getElementById('commentContent');
  const content = textarea.value.trim();

  if (!content && pendingAttachments.length === 0) {
    return;
  }

  // Disable input
  textarea.disabled = true;
  const sendBtn = document.querySelector('.btn-send');
  if (sendBtn) sendBtn.disabled = true;

  try {
    // First, submit the comment
    let commentId = null;
    if (content) {
      const response = await fetch(`/portal/api/tickets/${currentTicket.id}/comments`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json'
        },
        credentials: 'include',
        body: JSON.stringify({ content })
      });

      if (!response.ok) {
        const error = await response.json();
        throw new Error(error.error || 'Failed to add comment');
      }

      const comment = await response.json();
      commentId = comment.id;
    }

    // Then upload attachments if any
    if (pendingAttachments.length > 0) {
      const formData = new FormData();
      pendingAttachments.forEach(file => {
        formData.append('files', file);
      });
      if (commentId) {
        formData.append('commentId', commentId);
      }

      const attachResponse = await fetch(`/portal/api/tickets/${currentTicket.id}/attachments`, {
        method: 'POST',
        credentials: 'include',
        body: formData
      });

      if (!attachResponse.ok) {
        console.error('Failed to upload attachments');
      }
    }

    // Clear input and attachments
    textarea.value = '';
    textarea.style.height = 'auto';
    pendingAttachments = [];
    const previewsContainer = document.getElementById('attachmentPreviews');
    if (previewsContainer) previewsContainer.innerHTML = '';

  } catch (error) {
    console.error('Failed to add comment:', error);
    alert(error.message || 'Failed to add comment. Please try again.');
  } finally {
    textarea.disabled = false;
    if (sendBtn) sendBtn.disabled = false;
    textarea.focus();
  }
}

// =========================================================================
// Utility Functions
// =========================================================================

/**
 * Format date for display
 */
function formatDate(dateString) {
  if (!dateString) return '';

  const date = new Date(dateString);
  const now = new Date();
  const diffMs = now - date;
  const diffMins = Math.floor(diffMs / 60000);
  const diffHours = Math.floor(diffMs / 3600000);
  const diffDays = Math.floor(diffMs / 86400000);

  if (diffMins < 1) return 'just now';
  if (diffMins < 60) return `${diffMins}m ago`;
  if (diffHours < 24) return `${diffHours}h ago`;
  if (diffDays < 7) return `${diffDays}d ago`;

  return date.toLocaleDateString('en-US', {
    month: 'short',
    day: 'numeric',
    year: date.getFullYear() !== now.getFullYear() ? 'numeric' : undefined
  });
}

/**
 * Format status for display
 */
function formatStatus(status) {
  if (!status) return 'Open';
  return status.charAt(0).toUpperCase() + status.slice(1).replace(/_/g, ' ');
}

/**
 * Escape HTML to prevent XSS
 */
function escapeHtml(text) {
  if (!text) return '';
  const div = document.createElement('div');
  div.textContent = text;
  return div.innerHTML;
}
