import os

filepath = 'D:/Projects/Sentinel/server/internal/api/devices.go'

with open(filepath, 'r', encoding='utf-8') as f:
    content = f.read()

# Add log import if not present
if '"log"' not in content:
    content = content.replace(
        '"encoding/json"',
        '"encoding/json"\n\t"log"'
    )

# Fix 1: listDevices row scan error (lines 48-50)
content = content.replace(
    '''		if err != nil {
			continue
		}
		d.Tags = tags
		d.Metadata = metadata
		json.Unmarshal(gpuJSON, &d.GPU)
		json.Unmarshal(storageJSON, &d.Storage)''',
    '''		if err != nil {
			log.Printf("Error scanning device row: %v", err)
			continue
		}
		d.Tags = tags
		d.Metadata = metadata
		if err := json.Unmarshal(gpuJSON, &d.GPU); err != nil && len(gpuJSON) > 0 {
			log.Printf("Error unmarshaling GPU data for device %s: %v", d.ID, err)
		}
		if err := json.Unmarshal(storageJSON, &d.Storage); err != nil && len(storageJSON) > 0 {
			log.Printf("Error unmarshaling storage data for device %s: %v", d.ID, err)
		}'''
)

# Fix 2: getDevice json.Unmarshal errors (lines 103-104)
content = content.replace(
    '''	d.Tags = tags
	d.Metadata = metadata
	json.Unmarshal(gpuJSON, &d.GPU)
	json.Unmarshal(storageJSON, &d.Storage)

	if r.hub.IsAgentOnline(d.AgentID) {''',
    '''	d.Tags = tags
	d.Metadata = metadata
	if err := json.Unmarshal(gpuJSON, &d.GPU); err != nil && len(gpuJSON) > 0 {
		log.Printf("Error unmarshaling GPU data for device %s: %v", d.ID, err)
	}
	if err := json.Unmarshal(storageJSON, &d.Storage); err != nil && len(storageJSON) > 0 {
		log.Printf("Error unmarshaling storage data for device %s: %v", d.ID, err)
	}

	if r.hub.IsAgentOnline(d.AgentID) {'''
)

# Fix 3: getDeviceMetrics row scan error (lines 300-302)
content = content.replace(
    '''		err := rows.Scan(&m.Timestamp, &m.CPUPercent, &m.MemoryPercent, &m.MemoryUsedBytes,
			&m.MemoryTotalBytes, &m.DiskPercent, &m.DiskUsedBytes, &m.DiskTotalBytes,
			&m.NetworkRxBytes, &m.NetworkTxBytes, &m.ProcessCount)
		if err != nil {
			continue
		}
		metrics = append(metrics, m)''',
    '''		err := rows.Scan(&m.Timestamp, &m.CPUPercent, &m.MemoryPercent, &m.MemoryUsedBytes,
			&m.MemoryTotalBytes, &m.DiskPercent, &m.DiskUsedBytes, &m.DiskTotalBytes,
			&m.NetworkRxBytes, &m.NetworkTxBytes, &m.ProcessCount)
		if err != nil {
			log.Printf("Error scanning metrics row for device %s: %v", id, err)
			continue
		}
		metrics = append(metrics, m)'''
)

# Fix 4: executeCommand UPDATE status (lines 379-381) - add error logging
content = content.replace(
    '''	// Update command status to running
	r.db.Pool().Exec(ctx, `
		UPDATE commands SET status = 'running', started_at = NOW() WHERE id = $1
	`, commandID)

	c.JSON(http.StatusOK, gin.H{
		"commandId": commandID,''',
    '''	// Update command status to running
	if _, err := r.db.Pool().Exec(ctx, `
		UPDATE commands SET status = 'running', started_at = NOW() WHERE id = $1
	`, commandID); err != nil {
		log.Printf("Error updating command %s status to running: %v", commandID, err)
	}

	c.JSON(http.StatusOK, gin.H{
		"commandId": commandID,'''
)

# Fix 5: listDeviceCommands row scan error (lines 524-526)
content = content.replace(
    '''		err := rows.Scan(&cmd.ID, &cmd.DeviceID, &cmd.CommandType, &cmd.Command,
			&cmd.Status, &cmd.Output, &cmd.ErrorMessage, &cmd.ExitCode,
			&cmd.CreatedBy, &cmd.CreatedAt, &cmd.StartedAt, &cmd.CompletedAt)
		if err != nil {
			continue
		}
		commands = append(commands, cmd)
	}

	c.JSON(http.StatusOK, gin.H{
		"commands": commands,
		"total":    len(commands),
	})
}

// listCommands returns''',
    '''		err := rows.Scan(&cmd.ID, &cmd.DeviceID, &cmd.CommandType, &cmd.Command,
			&cmd.Status, &cmd.Output, &cmd.ErrorMessage, &cmd.ExitCode,
			&cmd.CreatedBy, &cmd.CreatedAt, &cmd.StartedAt, &cmd.CompletedAt)
		if err != nil {
			log.Printf("Error scanning command row for device %s: %v", id, err)
			continue
		}
		commands = append(commands, cmd)
	}

	c.JSON(http.StatusOK, gin.H{
		"commands": commands,
		"total":    len(commands),
	})
}

// listCommands returns'''
)

# Fix 6: listCommands row scan error (lines 571-574)
content = content.replace(
    '''		err := rows.Scan(&cmd.ID, &cmd.DeviceID, &cmd.CommandType, &cmd.Command,
			&cmd.Status, &cmd.Output, &cmd.ErrorMessage, &cmd.ExitCode,
			&cmd.CreatedBy, &cmd.CreatedAt, &cmd.StartedAt, &cmd.CompletedAt)
		if err != nil {
			continue
		}
		commands = append(commands, cmd)
	}

	c.JSON(http.StatusOK, gin.H{
		"commands": commands,
		"total":    len(commands),
	})
}

func (r *Router) getCommand''',
    '''		err := rows.Scan(&cmd.ID, &cmd.DeviceID, &cmd.CommandType, &cmd.Command,
			&cmd.Status, &cmd.Output, &cmd.ErrorMessage, &cmd.ExitCode,
			&cmd.CreatedBy, &cmd.CreatedAt, &cmd.StartedAt, &cmd.CompletedAt)
		if err != nil {
			log.Printf("Error scanning command row: %v", err)
			continue
		}
		commands = append(commands, cmd)
	}

	c.JSON(http.StatusOK, gin.H{
		"commands": commands,
		"total":    len(commands),
	})
}

func (r *Router) getCommand'''
)

# Fix 7: uninstallAgent UPDATE status (lines 657-659)
content = content.replace(
    '''	// Mark device as pending uninstall in database
	r.db.Pool().Exec(ctx, `
		UPDATE devices SET status = 'uninstalling', updated_at = NOW() WHERE id = $1
	`, id)

	c.JSON(http.StatusOK, gin.H{
		"message":   "Uninstall command sent to agent",''',
    '''	// Mark device as pending uninstall in database
	if _, err := r.db.Pool().Exec(ctx, `
		UPDATE devices SET status = 'uninstalling', updated_at = NOW() WHERE id = $1
	`, id); err != nil {
		log.Printf("Error updating device %s status to uninstalling: %v", id, err)
	}

	c.JSON(http.StatusOK, gin.H{
		"message":   "Uninstall command sent to agent",'''
)

# Fix 8: pingAgent UPDATE status offline (lines 693-695)
content = content.replace(
    '''	if !isOnline {
		// Agent is not connected to WebSocket - update status to offline
		r.db.Pool().Exec(ctx, `
			UPDATE devices SET status = 'offline', updated_at = NOW() WHERE id = $1
		`, id)

		// Broadcast offline status to dashboards''',
    '''	if !isOnline {
		// Agent is not connected to WebSocket - update status to offline
		if _, err := r.db.Pool().Exec(ctx, `
			UPDATE devices SET status = 'offline', updated_at = NOW() WHERE id = $1
		`, id); err != nil {
			log.Printf("Error updating device %s status to offline: %v", id, err)
		}

		// Broadcast offline status to dashboards'''
)

# Fix 9: pingAgent UPDATE status online (lines 731-733)
content = content.replace(
    '''	// Update device status to online and last_seen
	r.db.Pool().Exec(ctx, `
		UPDATE devices SET status = 'online', last_seen = NOW(), updated_at = NOW() WHERE id = $1
	`, id)

	// Broadcast online status to dashboards''',
    '''	// Update device status to online and last_seen
	if _, err := r.db.Pool().Exec(ctx, `
		UPDATE devices SET status = 'online', last_seen = NOW(), updated_at = NOW() WHERE id = $1
	`, id); err != nil {
		log.Printf("Error updating device %s status to online: %v", id, err)
	}

	// Broadcast online status to dashboards'''
)

with open(filepath, 'w', encoding='utf-8') as f:
    f.write(content)

print('Fixed all database error handling issues in devices.go')
