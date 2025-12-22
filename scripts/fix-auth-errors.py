import os

filepath = 'D:/Projects/Sentinel/server/internal/api/auth.go'

with open(filepath, 'r', encoding='utf-8') as f:
    content = f.read()

# Add log import if not present
if '"log"' not in content:
    content = content.replace(
        '"encoding/hex"',
        '"encoding/hex"\n\t"log"'
    )

# Fix 1: login - Update last login (line 98)
content = content.replace(
    '''\t// Update last login
\tr.db.Pool().Exec(ctx, "UPDATE users SET last_login = NOW() WHERE id = $1", user.ID)

\tc.JSON(http.StatusOK, LoginResponse{''',
    '''\t// Update last login
\tif _, err := r.db.Pool().Exec(ctx, "UPDATE users SET last_login = NOW() WHERE id = $1", user.ID); err != nil {
\t\tlog.Printf("Error updating last login for user %s: %v", user.ID, err)
\t}

\tc.JSON(http.StatusOK, LoginResponse{'''
)

# Fix 2: refreshToken - Delete expired session (line 144)
content = content.replace(
    '''\tif time.Now().After(session.ExpiresAt) {
\t\tr.db.Pool().Exec(ctx, "DELETE FROM sessions WHERE id = $1", session.ID)
\t\tc.JSON(http.StatusUnauthorized, gin.H{"error": "Refresh token expired"})
\t\treturn
\t}''',
    '''\tif time.Now().After(session.ExpiresAt) {
\t\tif _, err := r.db.Pool().Exec(ctx, "DELETE FROM sessions WHERE id = $1", session.ID); err != nil {
\t\t\tlog.Printf("Error deleting expired session %s: %v", session.ID, err)
\t\t}
\t\tc.JSON(http.StatusUnauthorized, gin.H{"error": "Refresh token expired"})
\t\treturn
\t}'''
)

# Fix 3: logout - Delete all sessions for user (line 183)
content = content.replace(
    '''\t// Delete all sessions for user
\tr.db.Pool().Exec(ctx, "DELETE FROM sessions WHERE user_id = $1", userID)

\tc.JSON(http.StatusOK, gin.H{"message": "Logged out successfully"})''',
    '''\t// Delete all sessions for user
\tif _, err := r.db.Pool().Exec(ctx, "DELETE FROM sessions WHERE user_id = $1", userID); err != nil {
\t\tlog.Printf("Error deleting sessions for user %s: %v", userID, err)
\t}

\tc.JSON(http.StatusOK, gin.H{"message": "Logged out successfully"})'''
)

with open(filepath, 'w', encoding='utf-8') as f:
    f.write(content)

print('Fixed all database error handling issues in auth.go')
