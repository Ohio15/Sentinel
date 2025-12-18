/**
 * Email Notification Service
 * Handles sending email notifications for ticket events
 */

import * as nodemailer from 'nodemailer';
import type { Database } from '../database';

export interface SMTPConfig {
  host: string;
  port: number;
  secure: boolean; // true for 465, false for other ports
  user: string;
  password: string;
  fromAddress: string;
  fromName?: string;
}

export interface EmailConfig {
  enabled: boolean;
  smtp?: SMTPConfig;
  portalUrl?: string; // Base URL for portal links in emails
}

export class EmailService {
  private transporter: nodemailer.Transporter | null = null;
  private config: EmailConfig;
  private database: Database;
  private processingInterval: NodeJS.Timeout | null = null;

  constructor(database: Database) {
    this.database = database;
    this.config = { enabled: false };
  }

  /**
   * Initialize the email service with configuration
   */
  async initialize(config: EmailConfig): Promise<void> {
    this.config = config;

    if (!config.enabled || !config.smtp) {
      console.log('[EmailService] Email notifications disabled');
      return;
    }

    try {
      this.transporter = nodemailer.createTransport({
        host: config.smtp.host,
        port: config.smtp.port,
        secure: config.smtp.secure,
        auth: {
          user: config.smtp.user,
          pass: config.smtp.password,
        },
      });

      // Verify connection
      await this.transporter.verify();
      console.log('[EmailService] SMTP connection verified');

      // Start processing queue
      this.startQueueProcessor();
    } catch (error) {
      console.error('[EmailService] Failed to initialize SMTP:', error);
      this.transporter = null;
    }
  }

  /**
   * Check if email service is ready
   */
  isReady(): boolean {
    return this.config.enabled && this.transporter !== null;
  }

  /**
   * Start the email queue processor
   */
  private startQueueProcessor(): void {
    if (this.processingInterval) {
      clearInterval(this.processingInterval);
    }

    // Process queue every 30 seconds
    this.processingInterval = setInterval(() => {
      this.processQueue().catch((err) => {
        console.error('[EmailService] Queue processing error:', err);
      });
    }, 30000);

    // Process immediately on startup
    this.processQueue().catch((err) => {
      console.error('[EmailService] Initial queue processing error:', err);
    });
  }

  /**
   * Process pending emails in the queue
   */
  private async processQueue(): Promise<void> {
    if (!this.isReady()) return;

    const pendingEmails = await this.database.getPendingEmails(10);

    for (const email of pendingEmails) {
      try {
        await this.sendEmail(email);
        await this.database.updateEmailStatus(email.id, 'sent');
        console.log('[EmailService] Email sent:', email.id, email.subject);
      } catch (error: any) {
        console.error('[EmailService] Failed to send email:', email.id, error.message);
        await this.database.updateEmailStatus(email.id, 'failed', error.message);
      }
    }
  }

  /**
   * Send a single email
   */
  private async sendEmail(email: any): Promise<void> {
    if (!this.transporter || !this.config.smtp) {
      throw new Error('Email service not configured');
    }

    // If using template, render it
    let htmlBody = email.bodyHtml;
    let textBody = email.bodyText;
    let subject = email.subject;

    if (email.templateName && email.templateData) {
      const template = await this.database.getEmailTemplate(email.templateName);
      if (template) {
        htmlBody = this.renderTemplate(template.bodyHtml, email.templateData);
        textBody = this.renderTemplate(template.bodyText || '', email.templateData);
        subject = this.renderTemplate(template.subject, email.templateData);
      }
    }

    const toAddresses = typeof email.toAddresses === 'string'
      ? JSON.parse(email.toAddresses)
      : email.toAddresses;

    const ccAddresses = typeof email.ccAddresses === 'string'
      ? JSON.parse(email.ccAddresses)
      : email.ccAddresses;

    const mailOptions: nodemailer.SendMailOptions = {
      from: this.config.smtp.fromName
        ? `"${this.config.smtp.fromName}" <${this.config.smtp.fromAddress}>`
        : this.config.smtp.fromAddress,
      to: toAddresses.join(', '),
      cc: ccAddresses?.length > 0 ? ccAddresses.join(', ') : undefined,
      subject,
      html: htmlBody || undefined,
      text: textBody || undefined,
    };

    await this.transporter.sendMail(mailOptions);
  }

  /**
   * Simple template renderer - replaces {{variable}} with values
   */
  private renderTemplate(template: string, data: any): string {
    if (!template) return '';

    return template.replace(/\{\{([^}]+)\}\}/g, (match, path) => {
      const keys = path.trim().split('.');
      let value: any = data;

      for (const key of keys) {
        if (value && typeof value === 'object' && key in value) {
          value = value[key];
        } else {
          return match; // Keep original if path not found
        }
      }

      return value !== undefined && value !== null ? String(value) : '';
    });
  }

  /**
   * Queue a ticket created notification
   */
  async notifyTicketCreated(ticket: any, assignedToEmail?: string): Promise<void> {
    if (!this.config.enabled) return;

    const toAddresses: string[] = [];

    // Send to assigned technician if set
    if (assignedToEmail) {
      toAddresses.push(assignedToEmail);
    }

    if (toAddresses.length === 0) {
      console.log('[EmailService] No recipients for ticket_created notification');
      return;
    }

    const templateData = {
      ticket: {
        number: ticket.ticketNumber,
        subject: ticket.subject,
        description: ticket.description,
        priority: ticket.priority,
        status: ticket.status,
      },
      submitter: {
        name: ticket.submitterName || ticket.requesterName,
        email: ticket.submitterEmail || ticket.requesterEmail,
      },
      portal: {
        ticketUrl: this.config.portalUrl
          ? `${this.config.portalUrl}/portal/tickets/${ticket.id}`
          : `#ticket-${ticket.id}`,
      },
    };

    await this.database.queueEmail({
      toAddresses,
      subject: `New Support Ticket #${ticket.ticketNumber}: ${ticket.subject}`,
      templateName: 'ticket_created',
      templateData,
    });
  }

  /**
   * Queue a ticket updated notification
   */
  async notifyTicketUpdated(ticket: any, updateMessage: string): Promise<void> {
    if (!this.config.enabled) return;

    const submitterEmail = ticket.submitterEmail || ticket.requesterEmail;
    if (!submitterEmail) {
      console.log('[EmailService] No submitter email for ticket_updated notification');
      return;
    }

    const templateData = {
      ticket: {
        number: ticket.ticketNumber,
        subject: ticket.subject,
        status: ticket.status,
      },
      update: {
        message: updateMessage,
      },
      portal: {
        ticketUrl: this.config.portalUrl
          ? `${this.config.portalUrl}/portal/tickets/${ticket.id}`
          : `#ticket-${ticket.id}`,
      },
    };

    await this.database.queueEmail({
      toAddresses: [submitterEmail],
      subject: `Ticket #${ticket.ticketNumber} Updated: ${ticket.subject}`,
      templateName: 'ticket_updated',
      templateData,
    });
  }

  /**
   * Queue a new comment notification
   */
  async notifyTicketComment(ticket: any, comment: any): Promise<void> {
    if (!this.config.enabled) return;

    // Don't send notification for internal comments to submitter
    if (comment.isInternal) return;

    const submitterEmail = ticket.submitterEmail || ticket.requesterEmail;
    if (!submitterEmail) {
      console.log('[EmailService] No submitter email for ticket_comment notification');
      return;
    }

    // Don't notify if the commenter is the submitter
    if (comment.authorEmail === submitterEmail) return;

    const templateData = {
      ticket: {
        number: ticket.ticketNumber,
        subject: ticket.subject,
      },
      comment: {
        author: comment.authorName,
        content: comment.content,
      },
      portal: {
        ticketUrl: this.config.portalUrl
          ? `${this.config.portalUrl}/portal/tickets/${ticket.id}`
          : `#ticket-${ticket.id}`,
      },
    };

    await this.database.queueEmail({
      toAddresses: [submitterEmail],
      subject: `New Comment on Ticket #${ticket.ticketNumber}: ${ticket.subject}`,
      templateName: 'ticket_comment',
      templateData,
    });
  }

  /**
   * Queue a ticket resolved notification
   */
  async notifyTicketResolved(ticket: any, resolution?: string): Promise<void> {
    if (!this.config.enabled) return;

    const submitterEmail = ticket.submitterEmail || ticket.requesterEmail;
    if (!submitterEmail) {
      console.log('[EmailService] No submitter email for ticket_resolved notification');
      return;
    }

    const templateData = {
      ticket: {
        number: ticket.ticketNumber,
        subject: ticket.subject,
        resolution: resolution || 'Issue has been resolved.',
      },
      portal: {
        ticketUrl: this.config.portalUrl
          ? `${this.config.portalUrl}/portal/tickets/${ticket.id}`
          : `#ticket-${ticket.id}`,
      },
    };

    await this.database.queueEmail({
      toAddresses: [submitterEmail],
      subject: `Ticket #${ticket.ticketNumber} Resolved: ${ticket.subject}`,
      templateName: 'ticket_resolved',
      templateData,
    });
  }

  /**
   * Send a custom email (immediate, not queued)
   */
  async sendCustomEmail(options: {
    to: string[];
    cc?: string[];
    subject: string;
    htmlBody?: string;
    textBody?: string;
  }): Promise<void> {
    if (!this.isReady() || !this.config.smtp) {
      throw new Error('Email service not configured');
    }

    const mailOptions: nodemailer.SendMailOptions = {
      from: this.config.smtp.fromName
        ? `"${this.config.smtp.fromName}" <${this.config.smtp.fromAddress}>`
        : this.config.smtp.fromAddress,
      to: options.to.join(', '),
      cc: options.cc?.join(', '),
      subject: options.subject,
      html: options.htmlBody,
      text: options.textBody,
    };

    await this.transporter!.sendMail(mailOptions);
  }

  /**
   * Update configuration at runtime
   */
  async updateConfig(config: EmailConfig): Promise<void> {
    // Stop current processor
    if (this.processingInterval) {
      clearInterval(this.processingInterval);
      this.processingInterval = null;
    }

    // Close existing transporter
    if (this.transporter) {
      this.transporter.close();
      this.transporter = null;
    }

    // Reinitialize with new config
    await this.initialize(config);
  }

  /**
   * Clean up resources
   */
  destroy(): void {
    if (this.processingInterval) {
      clearInterval(this.processingInterval);
      this.processingInterval = null;
    }

    if (this.transporter) {
      this.transporter.close();
      this.transporter = null;
    }
  }

  /**
   * Get current configuration (without secrets)
   */
  getConfig(): { enabled: boolean; smtp?: { host: string; port: number; fromAddress: string } } {
    return {
      enabled: this.config.enabled,
      smtp: this.config.smtp ? {
        host: this.config.smtp.host,
        port: this.config.smtp.port,
        fromAddress: this.config.smtp.fromAddress,
      } : undefined,
    };
  }
}
