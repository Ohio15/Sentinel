import os

filepath = 'D:/Projects/Sentinel/server/internal/api/agents.go'

with open(filepath, 'r', encoding='utf-8') as f:
    content = f.read()

# Add log import if not present
if '"log"' not in content:
    content = content.replace(
        '"encoding/hex"',
        '"encoding/hex"\n\t"log"'
    )

# Fix 1: listEnrollmentTokens - row scan error - line 54-56
content = content.replace(
    '''		if err != nil {
			continue
		}
		// Mask the token for display (show only first 8 chars)''',
    '''		if err != nil {
			log.Printf("Error scanning enrollment token row: %v", err)
			continue
		}
		// Mask the token for display (show only first 8 chars)'''
)

# Fix 2: downloadAgentInstaller - download log - lines 325-328
content = content.replace(
    '''	// Log download
	r.db.Pool().Exec(c.Request.Context(), `
		INSERT INTO agent_downloads (token_id, platform, architecture, ip_address, user_agent)
		VALUES ($1, $2, $3, $4, $5)
	`, tokenID, platform, arch, c.ClientIP(), c.Request.UserAgent())

	// Increment use count
	r.db.Pool().Exec(c.Request.Context(), `
		UPDATE enrollment_tokens SET use_count = use_count + 1 WHERE id = $1
	`, tokenID)

	// Generate unique agent ID for this download''',
    '''	// Log download
	if _, err := r.db.Pool().Exec(c.Request.Context(), `
		INSERT INTO agent_downloads (token_id, platform, architecture, ip_address, user_agent)
		VALUES ($1, $2, $3, $4, $5)
	`, tokenID, platform, arch, c.ClientIP(), c.Request.UserAgent()); err != nil {
		log.Printf("Error logging agent download: %v", err)
	}

	// Increment use count
	if _, err := r.db.Pool().Exec(c.Request.Context(), `
		UPDATE enrollment_tokens SET use_count = use_count + 1 WHERE id = $1
	`, tokenID); err != nil {
		log.Printf("Error incrementing token use count: %v", err)
	}

	// Generate unique agent ID for this download'''
)

with open(filepath, 'w', encoding='utf-8') as f:
    f.write(content)

print('Fixed all database error handling issues in agents.go')
