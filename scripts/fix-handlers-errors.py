import os

filepath = 'D:/Projects/Sentinel/server/internal/api/handlers.go'

with open(filepath, 'r', encoding='utf-8') as f:
    content = f.read()

# Fix 1: handleAgentWebSocket - line 156 - update device status online
content = content.replace(
    '''	// Update device status
	r.db.Pool().Exec(ctx, "UPDATE devices SET status = 'online', last_seen = NOW() WHERE id = $1", deviceID)

	// Broadcast online status to dashboards''',
    '''	// Update device status
	if _, err := r.db.Pool().Exec(ctx, "UPDATE devices SET status = 'online', last_seen = NOW() WHERE id = $1", deviceID); err != nil {
		log.Printf("Error updating device %s status to online: %v", deviceID, err)
	}

	// Broadcast online status to dashboards'''
)

# Fix 2: handleAgentWebSocket - line 173 - update device status offline
content = content.replace(
    '''	// Update device status on disconnect
	r.db.Pool().Exec(context.Background(), "UPDATE devices SET status = 'offline' WHERE id = $1", deviceID)

	// Broadcast offline status to dashboards''',
    '''	// Update device status on disconnect
	if _, err := r.db.Pool().Exec(context.Background(), "UPDATE devices SET status = 'offline' WHERE id = $1", deviceID); err != nil {
		log.Printf("Error updating device %s status to offline: %v", deviceID, err)
	}

	// Broadcast offline status to dashboards'''
)

# Fix 3: handleAgentMessage - heartbeat - lines 204-208
content = content.replace(
    '''		// Update last seen (and agent version if provided)
		if heartbeat.AgentVersion != "" {
			r.db.Pool().Exec(ctx, "UPDATE devices SET last_seen = NOW(), agent_version = $1 WHERE id = $2", heartbeat.AgentVersion, deviceID)
		} else {
			r.db.Pool().Exec(ctx, "UPDATE devices SET last_seen = NOW() WHERE id = $1", deviceID)
		}

		// Send ack back to agent''',
    '''		// Update last seen (and agent version if provided)
		if heartbeat.AgentVersion != "" {
			if _, err := r.db.Pool().Exec(ctx, "UPDATE devices SET last_seen = NOW(), agent_version = $1 WHERE id = $2", heartbeat.AgentVersion, deviceID); err != nil {
				log.Printf("Error updating device %s last_seen with version: %v", deviceID, err)
			}
		} else {
			if _, err := r.db.Pool().Exec(ctx, "UPDATE devices SET last_seen = NOW() WHERE id = $1", deviceID); err != nil {
				log.Printf("Error updating device %s last_seen: %v", deviceID, err)
			}
		}

		// Send ack back to agent'''
)

# Fix 4: handleAgentMessage - metrics insert - lines 231-240
content = content.replace(
    '''		// Insert metrics
		r.db.Pool().Exec(ctx, `
			INSERT INTO device_metrics (device_id, cpu_percent, memory_percent, memory_used_bytes,
				memory_total_bytes, disk_percent, disk_used_bytes, disk_total_bytes,
				network_rx_bytes, network_tx_bytes, process_count)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		`, deviceID, metrics.CPUPercent, metrics.MemoryPercent, metrics.MemoryUsedBytes,
			metrics.MemoryTotalBytes, metrics.DiskPercent, metrics.DiskUsedBytes,
			metrics.DiskTotalBytes, metrics.NetworkRxBytes, metrics.NetworkTxBytes,
			metrics.ProcessCount)

		// Broadcast to dashboards''',
    '''		// Insert metrics
		if _, err := r.db.Pool().Exec(ctx, `
			INSERT INTO device_metrics (device_id, cpu_percent, memory_percent, memory_used_bytes,
				memory_total_bytes, disk_percent, disk_used_bytes, disk_total_bytes,
				network_rx_bytes, network_tx_bytes, process_count)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		`, deviceID, metrics.CPUPercent, metrics.MemoryPercent, metrics.MemoryUsedBytes,
			metrics.MemoryTotalBytes, metrics.DiskPercent, metrics.DiskUsedBytes,
			metrics.DiskTotalBytes, metrics.NetworkRxBytes, metrics.NetworkTxBytes,
			metrics.ProcessCount); err != nil {
			log.Printf("Error inserting metrics for device %s: %v", deviceID, err)
		}

		// Broadcast to dashboards'''
)

# Fix 5: handleAgentMessage - command update - lines 281-284
content = content.replace(
    '''			r.db.Pool().Exec(ctx, `
				UPDATE commands SET status = $1, output = $2, error_message = $3, completed_at = NOW()
				WHERE id = $4
			`, status, data.Output, response.Error, data.CommandID)
		}

		// Forward response to dashboards with requestId preserved''',
    '''			if _, err := r.db.Pool().Exec(ctx, `
				UPDATE commands SET status = $1, output = $2, error_message = $3, completed_at = NOW()
				WHERE id = $4
			`, status, data.Output, response.Error, data.CommandID); err != nil {
				log.Printf("Error updating command %s status: %v", data.CommandID, err)
			}
		}

		// Forward response to dashboards with requestId preserved'''
)

# Fix 6: checkAlertRules - QueryRow without error check - line 393-397
content = content.replace(
    '''		if triggered {
			// Check cooldown (don't create duplicate alerts)
			var count int
			r.db.Pool().QueryRow(ctx, `
				SELECT COUNT(*) FROM alerts
				WHERE device_id = $1 AND rule_id = $2 AND status != 'resolved'
				AND created_at > NOW() - INTERVAL '15 minutes'
			`, deviceID, rule.ID).Scan(&count)

			if count == 0 {
				r.db.Pool().Exec(ctx, `
					INSERT INTO alerts (device_id, rule_id, severity, title, message)
					VALUES ($1, $2, $3, $4, $5)
				`, deviceID, rule.ID, rule.Severity, rule.Name,
					rule.Metric+" is "+rule.Operator+" "+fmt.Sprintf("%.2f", rule.Threshold))
			}
		}''',
    '''		if triggered {
			// Check cooldown (don't create duplicate alerts)
			var count int
			if err := r.db.Pool().QueryRow(ctx, `
				SELECT COUNT(*) FROM alerts
				WHERE device_id = $1 AND rule_id = $2 AND status != 'resolved'
				AND created_at > NOW() - INTERVAL '15 minutes'
			`, deviceID, rule.ID).Scan(&count); err != nil {
				log.Printf("Error checking alert cooldown for device %s: %v", deviceID, err)
				continue
			}

			if count == 0 {
				if _, err := r.db.Pool().Exec(ctx, `
					INSERT INTO alerts (device_id, rule_id, severity, title, message)
					VALUES ($1, $2, $3, $4, $5)
				`, deviceID, rule.ID, rule.Severity, rule.Name,
					rule.Metric+" is "+rule.Operator+" "+fmt.Sprintf("%.2f", rule.Threshold)); err != nil {
					log.Printf("Error creating alert for device %s: %v", deviceID, err)
				}
			}
		}'''
)

# Fix 7: checkAlertRules - row scan error - line 362-364
content = content.replace(
    '''		if err := rows.Scan(&rule.ID, &rule.Name, &rule.Metric, &rule.Operator, &rule.Threshold, &rule.Severity); err != nil {
			continue
		}

		var value float64''',
    '''		if err := rows.Scan(&rule.ID, &rule.Name, &rule.Metric, &rule.Operator, &rule.Threshold, &rule.Severity); err != nil {
			log.Printf("Error scanning alert rule row: %v", err)
			continue
		}

		var value float64'''
)

# Fix 8: listScripts - row scan error - line 454-456
content = content.replace(
    '''		if err := rows.Scan(&s.ID, &s.Name, &s.Description, &s.Language, &s.Content, &s.OSTypes, &s.CreatedAt, &s.UpdatedAt); err != nil {
			continue
		}
		scripts = append(scripts, map[string]interface{}{''',
    '''		if err := rows.Scan(&s.ID, &s.Name, &s.Description, &s.Language, &s.Content, &s.OSTypes, &s.CreatedAt, &s.UpdatedAt); err != nil {
			log.Printf("Error scanning script row: %v", err)
			continue
		}
		scripts = append(scripts, map[string]interface{}{'''
)

# Fix 9: executeScript - agent ID lookup - line 661-663
content = content.replace(
    '''	var agentID string
	r.db.Pool().QueryRow(ctx, "SELECT agent_id FROM devices WHERE id = $1", deviceID).Scan(&agentID)
	r.hub.SendToAgent(agentID, msg)

	c.JSON(http.StatusOK, gin.H{"commandId": commandID, "message": "Script execution started"})''',
    '''	var agentID string
	if err := r.db.Pool().QueryRow(ctx, "SELECT agent_id FROM devices WHERE id = $1", deviceID).Scan(&agentID); err != nil {
		log.Printf("Error looking up agent ID for device %s: %v", deviceID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to find device agent"})
		return
	}
	if err := r.hub.SendToAgent(agentID, msg); err != nil {
		log.Printf("Error sending script to agent %s: %v", agentID, err)
	}

	c.JSON(http.StatusOK, gin.H{"commandId": commandID, "message": "Script execution started"})'''
)

# Fix 10: listAlerts - row scan error - line 709-711
content = content.replace(
    '''		if err := rows.Scan(&a.ID, &a.DeviceID, &a.DeviceName, &a.RuleID, &a.Severity,
			&a.Title, &a.Message, &a.Status, &a.AcknowledgedAt, &a.ResolvedAt, &a.CreatedAt); err != nil {
			continue
		}

		alert := map[string]interface{}{''',
    '''		if err := rows.Scan(&a.ID, &a.DeviceID, &a.DeviceName, &a.RuleID, &a.Severity,
			&a.Title, &a.Message, &a.Status, &a.AcknowledgedAt, &a.ResolvedAt, &a.CreatedAt); err != nil {
			log.Printf("Error scanning alert row: %v", err)
			continue
		}

		alert := map[string]interface{}{'''
)

# Fix 11: acknowledgeAlert - exec without error check - line 781-784
content = content.replace(
    '''	r.db.Pool().Exec(ctx, `
		UPDATE alerts SET status = 'acknowledged', acknowledged_by = $1, acknowledged_at = NOW()
		WHERE id = $2
	`, userID, id)

	c.JSON(http.StatusOK, gin.H{"message": "Alert acknowledged"})''',
    '''	if _, err := r.db.Pool().Exec(ctx, `
		UPDATE alerts SET status = 'acknowledged', acknowledged_by = $1, acknowledged_at = NOW()
		WHERE id = $2
	`, userID, id); err != nil {
		log.Printf("Error acknowledging alert %s: %v", id, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to acknowledge alert"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Alert acknowledged"})'''
)

# Fix 12: resolveAlert - exec without error check - line 793
content = content.replace(
    '''	r.db.Pool().Exec(ctx, "UPDATE alerts SET status = 'resolved', resolved_at = NOW() WHERE id = $1", id)

	c.JSON(http.StatusOK, gin.H{"message": "Alert resolved"})
}

// Alert rules handlers''',
    '''	if _, err := r.db.Pool().Exec(ctx, "UPDATE alerts SET status = 'resolved', resolved_at = NOW() WHERE id = $1", id); err != nil {
		log.Printf("Error resolving alert %s: %v", id, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to resolve alert"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Alert resolved"})
}

// Alert rules handlers'''
)

# Fix 13: listAlertRules - row scan error - line 827-829
content = content.replace(
    '''		if err := rows.Scan(&rule.ID, &rule.Name, &rule.Description, &rule.Enabled, &rule.Metric,
			&rule.Operator, &rule.Threshold, &rule.Severity, &rule.CooldownMinutes, &rule.CreatedAt); err != nil {
			continue
		}
		rules = append(rules, map[string]interface{}{''',
    '''		if err := rows.Scan(&rule.ID, &rule.Name, &rule.Description, &rule.Enabled, &rule.Metric,
			&rule.Operator, &rule.Threshold, &rule.Severity, &rule.CooldownMinutes, &rule.CreatedAt); err != nil {
			log.Printf("Error scanning alert rule row: %v", err)
			continue
		}
		rules = append(rules, map[string]interface{}{'''
)

# Fix 14: getSettings - row scan error - line 996-1000
content = content.replace(
    '''		if err := rows.Scan(&key, &value); err != nil {
			continue
		}
		settings[key] = value
	}

	c.JSON(http.StatusOK, settings)
}

func (r *Router) updateSettings''',
    '''		if err := rows.Scan(&key, &value); err != nil {
			log.Printf("Error scanning settings row: %v", err)
			continue
		}
		settings[key] = value
	}

	c.JSON(http.StatusOK, settings)
}

func (r *Router) updateSettings'''
)

# Fix 15: listUsers - row scan error - line 1055-1057
content = content.replace(
    '''		if err := rows.Scan(&u.ID, &u.Email, &u.FirstName, &u.LastName, &u.Role,
			&u.IsActive, &u.LastLogin, &u.CreatedAt); err != nil {
			continue
		}
		users = append(users, map[string]interface{}{''',
    '''		if err := rows.Scan(&u.ID, &u.Email, &u.FirstName, &u.LastName, &u.Role,
			&u.IsActive, &u.LastLogin, &u.CreatedAt); err != nil {
			log.Printf("Error scanning user row: %v", err)
			continue
		}
		users = append(users, map[string]interface{}{'''
)

# Fix 16: getDashboardStats - all the QueryRow.Scan calls - lines 1219-1255
content = content.replace(
    '''	// Total devices
	var totalDevices int
	r.db.Pool().QueryRow(ctx, "SELECT COUNT(*) FROM devices").Scan(&totalDevices)
	stats["totalDevices"] = totalDevices

	// Online devices
	var onlineDevices int
	r.db.Pool().QueryRow(ctx, "SELECT COUNT(*) FROM devices WHERE status = 'online'").Scan(&onlineDevices)
	stats["onlineDevices"] = onlineDevices

	// Offline devices
	var offlineDevices int
	r.db.Pool().QueryRow(ctx, "SELECT COUNT(*) FROM devices WHERE status = 'offline'").Scan(&offlineDevices)
	stats["offlineDevices"] = offlineDevices

	// Critical alerts
	var criticalAlerts int
	r.db.Pool().QueryRow(ctx, "SELECT COUNT(*) FROM alerts WHERE severity = 'critical' AND status = 'open'").Scan(&criticalAlerts)
	stats["criticalAlerts"] = criticalAlerts

	// Warning alerts
	var warningAlerts int
	r.db.Pool().QueryRow(ctx, "SELECT COUNT(*) FROM alerts WHERE severity = 'warning' AND status = 'open'").Scan(&warningAlerts)
	stats["warningAlerts"] = warningAlerts

	// Total alerts
	var totalAlerts int
	r.db.Pool().QueryRow(ctx, "SELECT COUNT(*) FROM alerts WHERE status = 'open'").Scan(&totalAlerts)
	stats["totalAlerts"] = totalAlerts

	// Total scripts
	var totalScripts int
	r.db.Pool().QueryRow(ctx, "SELECT COUNT(*) FROM scripts").Scan(&totalScripts)
	stats["totalScripts"] = totalScripts

	// Total users
	var totalUsers int
	r.db.Pool().QueryRow(ctx, "SELECT COUNT(*) FROM users WHERE is_active = true").Scan(&totalUsers)
	stats["totalUsers"] = totalUsers

	c.JSON(http.StatusOK, stats)''',
    '''	// Total devices
	var totalDevices int
	if err := r.db.Pool().QueryRow(ctx, "SELECT COUNT(*) FROM devices").Scan(&totalDevices); err != nil {
		log.Printf("Error getting total devices count: %v", err)
	}
	stats["totalDevices"] = totalDevices

	// Online devices
	var onlineDevices int
	if err := r.db.Pool().QueryRow(ctx, "SELECT COUNT(*) FROM devices WHERE status = 'online'").Scan(&onlineDevices); err != nil {
		log.Printf("Error getting online devices count: %v", err)
	}
	stats["onlineDevices"] = onlineDevices

	// Offline devices
	var offlineDevices int
	if err := r.db.Pool().QueryRow(ctx, "SELECT COUNT(*) FROM devices WHERE status = 'offline'").Scan(&offlineDevices); err != nil {
		log.Printf("Error getting offline devices count: %v", err)
	}
	stats["offlineDevices"] = offlineDevices

	// Critical alerts
	var criticalAlerts int
	if err := r.db.Pool().QueryRow(ctx, "SELECT COUNT(*) FROM alerts WHERE severity = 'critical' AND status = 'open'").Scan(&criticalAlerts); err != nil {
		log.Printf("Error getting critical alerts count: %v", err)
	}
	stats["criticalAlerts"] = criticalAlerts

	// Warning alerts
	var warningAlerts int
	if err := r.db.Pool().QueryRow(ctx, "SELECT COUNT(*) FROM alerts WHERE severity = 'warning' AND status = 'open'").Scan(&warningAlerts); err != nil {
		log.Printf("Error getting warning alerts count: %v", err)
	}
	stats["warningAlerts"] = warningAlerts

	// Total alerts
	var totalAlerts int
	if err := r.db.Pool().QueryRow(ctx, "SELECT COUNT(*) FROM alerts WHERE status = 'open'").Scan(&totalAlerts); err != nil {
		log.Printf("Error getting total alerts count: %v", err)
	}
	stats["totalAlerts"] = totalAlerts

	// Total scripts
	var totalScripts int
	if err := r.db.Pool().QueryRow(ctx, "SELECT COUNT(*) FROM scripts").Scan(&totalScripts); err != nil {
		log.Printf("Error getting total scripts count: %v", err)
	}
	stats["totalScripts"] = totalScripts

	// Total users
	var totalUsers int
	if err := r.db.Pool().QueryRow(ctx, "SELECT COUNT(*) FROM users WHERE is_active = true").Scan(&totalUsers); err != nil {
		log.Printf("Error getting total users count: %v", err)
	}
	stats["totalUsers"] = totalUsers

	c.JSON(http.StatusOK, stats)'''
)

with open(filepath, 'w', encoding='utf-8') as f:
    f.write(content)

print('Fixed all database error handling issues in handlers.go')
