import os

# Fix router.go to integrate CSRF middleware
router_file = 'D:/Projects/Sentinel/server/internal/api/router.go'

with open(router_file, 'r', encoding='utf-8') as f:
    content = f.read()

# Update CORS to include X-CSRF-Token header
if 'X-CSRF-Token' not in content:
    content = content.replace(
        'c.Header("Access-Control-Allow-Headers", "Origin, Content-Type, Authorization, X-Enrollment-Token, X-Agent-Token")',
        'c.Header("Access-Control-Allow-Headers", "Origin, Content-Type, Authorization, X-Enrollment-Token, X-Agent-Token, X-CSRF-Token")'
    )
    print('Added X-CSRF-Token to CORS allowed headers')

# Add CSRF middleware to first NewRouter function
if 'CSRFMiddleware' not in content:
    # Add to first protected route group
    content = content.replace(
        '''// Protected routes (require JWT)
\t\tprotected := api.Group("")
\t\tprotected.Use(middleware.AuthMiddleware(cfg.JWTSecret))
\t\t{
\t\t\t// Auth
\t\t\tprotected.POST("/auth/logout", router.logout)''',
        '''// Protected routes (require JWT)
\t\tprotected := api.Group("")
\t\tprotected.Use(middleware.AuthMiddleware(cfg.JWTSecret))
\t\tprotected.Use(middleware.CSRFMiddleware(middleware.DefaultCSRFConfig()))
\t\t{
\t\t\t// Auth
\t\t\tprotected.POST("/auth/logout", router.logout)'''
    )

    # Add to second NewRouterWithServices function
    content = content.replace(
        '''// Protected routes (require JWT)
\t\tprotected := api.Group("")
\t\tprotected.Use(middleware.AuthMiddleware(services.Config.JWTSecret))
\t\t{
\t\t\t// Auth
\t\t\tprotected.POST("/auth/logout", logoutHandler(services))''',
        '''// Protected routes (require JWT)
\t\tprotected := api.Group("")
\t\tprotected.Use(middleware.AuthMiddleware(services.Config.JWTSecret))
\t\tprotected.Use(middleware.CSRFMiddleware(middleware.DefaultCSRFConfig()))
\t\t{
\t\t\t// Auth
\t\t\tprotected.POST("/auth/logout", logoutHandler(services))'''
    )
    print('Added CSRF middleware to protected routes')

with open(router_file, 'w', encoding='utf-8') as f:
    f.write(content)

# Update auth.go to set CSRF token on login
auth_file = 'D:/Projects/Sentinel/server/internal/api/auth.go'

with open(auth_file, 'r', encoding='utf-8') as f:
    content = f.read()

# Add middleware import if needed
if '"github.com/sentinel/server/internal/middleware"' not in content:
    content = content.replace(
        '"github.com/sentinel/server/internal/middleware"',
        '"github.com/sentinel/server/internal/middleware"'
    )
    # If import doesn't exist, add it
    if '"github.com/sentinel/server/internal/middleware"' not in content:
        content = content.replace(
            '"golang.org/x/crypto/bcrypt"',
            '"github.com/sentinel/server/internal/middleware"\n\t"golang.org/x/crypto/bcrypt"'
        )
        print('Added middleware import to auth.go')

# Update login response to include CSRF token
if 'csrfToken' not in content and 'CSRFToken' not in content:
    content = content.replace(
        '''\tc.JSON(http.StatusOK, LoginResponse{
\t\tAccessToken:  accessToken,
\t\tRefreshToken: refreshToken,
\t\tExpiresIn:    3600, // 1 hour
\t\tUser: UserResponse{''',
        '''\t// Generate new CSRF token on login
\tcsrfConfig := middleware.DefaultCSRFConfig()
\tcsrfToken := middleware.SetNewCSRFToken(c, csrfConfig)

\tc.JSON(http.StatusOK, gin.H{
\t\t"accessToken":  accessToken,
\t\t"refreshToken": refreshToken,
\t\t"expiresIn":    3600,
\t\t"csrfToken":    csrfToken,
\t\t"user": UserResponse{'''
    )

    # Fix the closing brackets
    content = content.replace(
        '''\t\t\tLastName:  user.LastName,
\t\t\tRole:      user.Role,
\t\t},
\t})
}

func (r *Router) refreshToken(c *gin.Context) {''',
        '''\t\t\tLastName:  user.LastName,
\t\t\tRole:      user.Role,
\t\t},
\t})
}

func (r *Router) refreshToken(c *gin.Context) {'''
    )
    print('Updated login handler to set CSRF token')

with open(auth_file, 'w', encoding='utf-8') as f:
    f.write(content)

print('CSRF integration complete!')

# Verify the server still compiles
import subprocess
result = subprocess.run(['go', 'build', './...'], cwd='D:/Projects/Sentinel/server', capture_output=True, text=True)
if result.returncode == 0:
    print('Server compiles successfully with CSRF changes!')
else:
    print('Compilation error:')
    print(result.stderr)
