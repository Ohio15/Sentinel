/**
 * Sentinel Support Portal - Frontend Application
 */

// State
let currentUser = null;
let clientBranding = null;
let tickets = [];
let currentTicket = null;

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
    renderTicketDetail();
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
        <span class="ticket-number">#${ticket.ticketNumber || ticket.id.slice(0, 8)}</span>
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
  ticketTitle.textContent = `Ticket #${ticket.ticketNumber || ticket.id.slice(0, 8)}`;

  // Build info items for submitter and device
  let infoItems = '';
  if (ticket.submitterName || ticket.submitterEmail) {
    infoItems += `<span><strong>Submitted by:</strong> ${escapeHtml(ticket.submitterName || ticket.submitterEmail)}</span>`;
  }
  if (ticket.userDeviceName) {
    infoItems += `<span><strong>Device:</strong> ${escapeHtml(ticket.userDeviceName)}</span>`;
  }

  ticketDetail.innerHTML = `
    <div class="ticket-detail-header">
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
    <div class="comments-section">
      <h3>Comments</h3>
      <div class="comments-list" id="commentsList">
        ${renderComments(ticket.comments || [])}
      </div>
      ${ticket.status !== 'closed' ? `
        <div class="comment-form">
          <h4>Add a Comment</h4>
          <textarea id="commentContent" placeholder="Type your comment here..." rows="4"></textarea>
          <button class="btn btn-primary" onclick="submitComment()">Post Comment</button>
        </div>
      ` : ''}
    </div>
  `;
}

/**
 * Render comments list
 */
function renderComments(comments) {
  if (comments.length === 0) {
    return '<p style="color: var(--secondary); text-align: center; padding: 1rem;">No comments yet.</p>';
  }

  return comments.map(comment => `
    <div class="comment">
      <div class="comment-header">
        <span class="comment-author">${escapeHtml(comment.authorName || 'Support')}</span>
        <span class="comment-date">${formatDate(comment.createdAt)}</span>
      </div>
      <div class="comment-content">${escapeHtml(comment.content)}</div>
    </div>
  `).join('');
}

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
 * Submit comment
 */
async function submitComment() {
  const content = document.getElementById('commentContent').value.trim();

  if (!content) {
    alert('Please enter a comment');
    return;
  }

  try {
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

    // Reload ticket to show new comment
    await showTicketDetail(currentTicket.id);
  } catch (error) {
    console.error('Failed to add comment:', error);
    alert(error.message || 'Failed to add comment. Please try again.');
  }
}

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
