package api

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// Knowledge Base handlers

// KB Category handlers

func (r *Router) listKBCategories(c *gin.Context) {
	ctx := context.Background()
	activeOnly := c.Query("active") == "true"

	var query string
	if activeOnly {
		query = `
			SELECT id, name, slug, description, parent_id, icon, color, sort_order, is_active, article_count, created_at, updated_at
			FROM kb_categories WHERE is_active = TRUE ORDER BY sort_order, name
		`
	} else {
		query = `
			SELECT id, name, slug, description, parent_id, icon, color, sort_order, is_active, article_count, created_at, updated_at
			FROM kb_categories ORDER BY sort_order, name
		`
	}

	rows, err := r.db.Pool().Query(ctx, query)
	if err != nil {
		log.Printf("Error listing KB categories: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch KB categories"})
		return
	}
	defer rows.Close()

	categories := make([]map[string]interface{}, 0)
	for rows.Next() {
		var id uuid.UUID
		var name, slug string
		var description, icon, color *string
		var parentID *uuid.UUID
		var sortOrder, articleCount int
		var isActive bool
		var createdAt, updatedAt time.Time

		if err := rows.Scan(&id, &name, &slug, &description, &parentID, &icon, &color,
			&sortOrder, &isActive, &articleCount, &createdAt, &updatedAt); err != nil {
			log.Printf("Error scanning KB category row: %v", err)
			continue
		}
		categories = append(categories, map[string]interface{}{
			"id":           id,
			"name":         name,
			"slug":         slug,
			"description":  description,
			"parentId":     parentID,
			"icon":         icon,
			"color":        color,
			"sortOrder":    sortOrder,
			"isActive":     isActive,
			"articleCount": articleCount,
			"createdAt":    createdAt,
			"updatedAt":    updatedAt,
		})
	}

	c.JSON(http.StatusOK, categories)
}

func (r *Router) getKBCategory(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid category ID"})
		return
	}

	ctx := context.Background()
	var category struct {
		ID           uuid.UUID
		Name         string
		Slug         string
		Description  *string
		ParentID     *uuid.UUID
		Icon         *string
		Color        *string
		SortOrder    int
		IsActive     bool
		ArticleCount int
		CreatedAt    time.Time
		UpdatedAt    time.Time
	}

	err = r.db.Pool().QueryRow(ctx, `
		SELECT id, name, slug, description, parent_id, icon, color, sort_order, is_active, article_count, created_at, updated_at
		FROM kb_categories WHERE id = $1
	`, id).Scan(&category.ID, &category.Name, &category.Slug, &category.Description, &category.ParentID,
		&category.Icon, &category.Color, &category.SortOrder, &category.IsActive, &category.ArticleCount,
		&category.CreatedAt, &category.UpdatedAt)

	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Category not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"id":           category.ID,
		"name":         category.Name,
		"slug":         category.Slug,
		"description":  category.Description,
		"parentId":     category.ParentID,
		"icon":         category.Icon,
		"color":        category.Color,
		"sortOrder":    category.SortOrder,
		"isActive":     category.IsActive,
		"articleCount": category.ArticleCount,
		"createdAt":    category.CreatedAt,
		"updatedAt":    category.UpdatedAt,
	})
}

func (r *Router) createKBCategory(c *gin.Context) {
	var req struct {
		Name        string  `json:"name" binding:"required"`
		Slug        string  `json:"slug"`
		Description string  `json:"description"`
		ParentID    *string `json:"parentId"`
		Icon        string  `json:"icon"`
		Color       string  `json:"color"`
		SortOrder   *int    `json:"sortOrder"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx := context.Background()

	var parentID *uuid.UUID
	if req.ParentID != nil && *req.ParentID != "" {
		parsed, err := uuid.Parse(*req.ParentID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid parent ID"})
			return
		}
		parentID = &parsed
	}

	icon := "folder"
	if req.Icon != "" {
		icon = req.Icon
	}

	color := "#6B7280"
	if req.Color != "" {
		color = req.Color
	}

	sortOrder := 0
	if req.SortOrder != nil {
		sortOrder = *req.SortOrder
	}

	// Generate slug from name if not provided
	slug := req.Slug
	if slug == "" {
		err := r.db.Pool().QueryRow(ctx, "SELECT generate_kb_slug($1)", req.Name).Scan(&slug)
		if err != nil {
			slug = req.Name
		}
	}

	var id uuid.UUID
	err := r.db.Pool().QueryRow(ctx, `
		INSERT INTO kb_categories (name, slug, description, parent_id, icon, color, sort_order)
		VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id
	`, req.Name, slug, req.Description, parentID, icon, color, sortOrder).Scan(&id)

	if err != nil {
		log.Printf("Error creating KB category: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create category"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"id": id, "name": req.Name, "slug": slug})
}

func (r *Router) updateKBCategory(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid category ID"})
		return
	}

	var req struct {
		Name        string  `json:"name"`
		Slug        string  `json:"slug"`
		Description string  `json:"description"`
		ParentID    *string `json:"parentId"`
		Icon        string  `json:"icon"`
		Color       string  `json:"color"`
		SortOrder   *int    `json:"sortOrder"`
		IsActive    *bool   `json:"isActive"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx := context.Background()

	var parentID *uuid.UUID
	if req.ParentID != nil {
		if *req.ParentID != "" {
			parsed, err := uuid.Parse(*req.ParentID)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid parent ID"})
				return
			}
			parentID = &parsed
		}
	}

	_, err = r.db.Pool().Exec(ctx, `
		UPDATE kb_categories SET
			name = COALESCE(NULLIF($1, ''), name),
			slug = COALESCE(NULLIF($2, ''), slug),
			description = COALESCE(NULLIF($3, ''), description),
			parent_id = $4,
			icon = COALESCE(NULLIF($5, ''), icon),
			color = COALESCE(NULLIF($6, ''), color),
			sort_order = COALESCE($7, sort_order),
			is_active = COALESCE($8, is_active),
			updated_at = NOW()
		WHERE id = $9
	`, req.Name, req.Slug, req.Description, parentID, req.Icon, req.Color, req.SortOrder, req.IsActive, id)

	if err != nil {
		log.Printf("Error updating KB category: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update category"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Category updated successfully"})
}

func (r *Router) deleteKBCategory(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid category ID"})
		return
	}

	ctx := context.Background()
	result, err := r.db.Pool().Exec(ctx, "DELETE FROM kb_categories WHERE id = $1", id)
	if err != nil {
		log.Printf("Error deleting KB category: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete category"})
		return
	}

	if result.RowsAffected() == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "Category not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Category deleted successfully"})
}

// KB Article handlers

func (r *Router) listKBArticles(c *gin.Context) {
	ctx := context.Background()
	categoryID := c.Query("categoryId")
	status := c.Query("status")
	search := c.Query("search")
	featured := c.Query("featured")

	query := `
		SELECT id, title, slug, summary, category_id, status, is_featured, is_pinned,
			tags, view_count, helpful_count, not_helpful_count, author_name, published_at, created_at, updated_at
		FROM kb_articles WHERE 1=1
	`
	args := []interface{}{}
	argNum := 1

	if categoryID != "" {
		parsed, err := uuid.Parse(categoryID)
		if err == nil {
			query += " AND category_id = $" + string(rune('0'+argNum))
			args = append(args, parsed)
			argNum++
		}
	}

	if status != "" {
		query += " AND status = $" + string(rune('0'+argNum))
		args = append(args, status)
		argNum++
	}

	if featured == "true" {
		query += " AND is_featured = TRUE"
	}

	if search != "" {
		query += " AND (title ILIKE $" + string(rune('0'+argNum)) + " OR summary ILIKE $" + string(rune('0'+argNum)) + ")"
		args = append(args, "%"+search+"%")
		argNum++
	}

	query += " ORDER BY is_pinned DESC, published_at DESC NULLS LAST, created_at DESC"

	rows, err := r.db.Pool().Query(ctx, query, args...)
	if err != nil {
		log.Printf("Error listing KB articles: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch articles"})
		return
	}
	defer rows.Close()

	articles := make([]map[string]interface{}, 0)
	for rows.Next() {
		var id uuid.UUID
		var title, slug string
		var summary *string
		var categoryID *uuid.UUID
		var status string
		var isFeatured, isPinned bool
		var tagsJSON []byte
		var viewCount, helpfulCount, notHelpfulCount int
		var authorName *string
		var publishedAt *time.Time
		var createdAt, updatedAt time.Time

		if err := rows.Scan(&id, &title, &slug, &summary, &categoryID, &status, &isFeatured, &isPinned,
			&tagsJSON, &viewCount, &helpfulCount, &notHelpfulCount, &authorName, &publishedAt, &createdAt, &updatedAt); err != nil {
			log.Printf("Error scanning KB article row: %v", err)
			continue
		}

		var tags []string
		json.Unmarshal(tagsJSON, &tags)

		articles = append(articles, map[string]interface{}{
			"id":              id,
			"title":           title,
			"slug":            slug,
			"summary":         summary,
			"categoryId":      categoryID,
			"status":          status,
			"isFeatured":      isFeatured,
			"isPinned":        isPinned,
			"tags":            tags,
			"viewCount":       viewCount,
			"helpfulCount":    helpfulCount,
			"notHelpfulCount": notHelpfulCount,
			"authorName":      authorName,
			"publishedAt":     publishedAt,
			"createdAt":       createdAt,
			"updatedAt":       updatedAt,
		})
	}

	c.JSON(http.StatusOK, articles)
}

func (r *Router) getKBArticle(c *gin.Context) {
	idOrSlug := c.Param("id")
	ctx := context.Background()

	var query string
	var arg interface{}

	// Try parsing as UUID first
	if parsedID, err := uuid.Parse(idOrSlug); err == nil {
		query = `SELECT id, title, slug, content, content_html, summary, category_id, status, is_featured, is_pinned,
			tags, keywords, view_count, helpful_count, not_helpful_count, author_name, author_email,
			last_reviewed_at, last_reviewed_by, published_at, created_at, updated_at
			FROM kb_articles WHERE id = $1`
		arg = parsedID
	} else {
		// Assume it's a slug
		query = `SELECT id, title, slug, content, content_html, summary, category_id, status, is_featured, is_pinned,
			tags, keywords, view_count, helpful_count, not_helpful_count, author_name, author_email,
			last_reviewed_at, last_reviewed_by, published_at, created_at, updated_at
			FROM kb_articles WHERE slug = $1`
		arg = idOrSlug
	}

	var id uuid.UUID
	var title, slug string
	var content, contentHTML, summary *string
	var categoryID *uuid.UUID
	var status string
	var isFeatured, isPinned bool
	var tagsJSON, keywordsJSON []byte
	var viewCount, helpfulCount, notHelpfulCount int
	var authorName, authorEmail, lastReviewedBy *string
	var lastReviewedAt, publishedAt *time.Time
	var createdAt, updatedAt time.Time

	err := r.db.Pool().QueryRow(ctx, query, arg).Scan(
		&id, &title, &slug, &content, &contentHTML, &summary, &categoryID, &status, &isFeatured, &isPinned,
		&tagsJSON, &keywordsJSON, &viewCount, &helpfulCount, &notHelpfulCount, &authorName, &authorEmail,
		&lastReviewedAt, &lastReviewedBy, &publishedAt, &createdAt, &updatedAt,
	)

	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Article not found"})
		return
	}

	var tags, keywords []string
	json.Unmarshal(tagsJSON, &tags)
	json.Unmarshal(keywordsJSON, &keywords)

	c.JSON(http.StatusOK, gin.H{
		"id":              id,
		"title":           title,
		"slug":            slug,
		"content":         content,
		"contentHtml":     contentHTML,
		"summary":         summary,
		"categoryId":      categoryID,
		"status":          status,
		"isFeatured":      isFeatured,
		"isPinned":        isPinned,
		"tags":            tags,
		"keywords":        keywords,
		"viewCount":       viewCount,
		"helpfulCount":    helpfulCount,
		"notHelpfulCount": notHelpfulCount,
		"authorName":      authorName,
		"authorEmail":     authorEmail,
		"lastReviewedAt":  lastReviewedAt,
		"lastReviewedBy":  lastReviewedBy,
		"publishedAt":     publishedAt,
		"createdAt":       createdAt,
		"updatedAt":       updatedAt,
	})
}

func (r *Router) createKBArticle(c *gin.Context) {
	var req struct {
		Title       string   `json:"title" binding:"required"`
		Slug        string   `json:"slug"`
		Content     string   `json:"content"`
		ContentHTML string   `json:"contentHtml"`
		Summary     string   `json:"summary"`
		CategoryID  *string  `json:"categoryId"`
		Status      string   `json:"status"`
		IsFeatured  *bool    `json:"isFeatured"`
		IsPinned    *bool    `json:"isPinned"`
		Tags        []string `json:"tags"`
		Keywords    []string `json:"keywords"`
		AuthorName  string   `json:"authorName"`
		AuthorEmail string   `json:"authorEmail"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx := context.Background()

	var categoryID *uuid.UUID
	if req.CategoryID != nil && *req.CategoryID != "" {
		parsed, err := uuid.Parse(*req.CategoryID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid category ID"})
			return
		}
		categoryID = &parsed
	}

	status := "draft"
	if req.Status != "" {
		status = req.Status
	}

	isFeatured := false
	if req.IsFeatured != nil {
		isFeatured = *req.IsFeatured
	}

	isPinned := false
	if req.IsPinned != nil {
		isPinned = *req.IsPinned
	}

	// Generate slug from title if not provided
	slug := req.Slug
	if slug == "" {
		err := r.db.Pool().QueryRow(ctx, "SELECT generate_kb_slug($1)", req.Title).Scan(&slug)
		if err != nil {
			slug = req.Title
		}
	}

	tags := req.Tags
	if tags == nil {
		tags = []string{}
	}
	tagsJSON, _ := json.Marshal(tags)

	keywords := req.Keywords
	if keywords == nil {
		keywords = []string{}
	}
	keywordsJSON, _ := json.Marshal(keywords)

	var publishedAt *time.Time
	if status == "published" {
		now := time.Now()
		publishedAt = &now
	}

	var id uuid.UUID
	err := r.db.Pool().QueryRow(ctx, `
		INSERT INTO kb_articles (title, slug, content, content_html, summary, category_id, status, is_featured, is_pinned,
			tags, keywords, author_name, author_email, published_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
		RETURNING id
	`, req.Title, slug, req.Content, req.ContentHTML, req.Summary, categoryID, status, isFeatured, isPinned,
		tagsJSON, keywordsJSON, req.AuthorName, req.AuthorEmail, publishedAt).Scan(&id)

	if err != nil {
		log.Printf("Error creating KB article: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create article"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"id": id, "title": req.Title, "slug": slug})
}

func (r *Router) updateKBArticle(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid article ID"})
		return
	}

	var req struct {
		Title          string   `json:"title"`
		Slug           string   `json:"slug"`
		Content        string   `json:"content"`
		ContentHTML    string   `json:"contentHtml"`
		Summary        string   `json:"summary"`
		CategoryID     *string  `json:"categoryId"`
		Status         string   `json:"status"`
		IsFeatured     *bool    `json:"isFeatured"`
		IsPinned       *bool    `json:"isPinned"`
		Tags           []string `json:"tags"`
		Keywords       []string `json:"keywords"`
		LastReviewedBy string   `json:"lastReviewedBy"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx := context.Background()

	var categoryID *uuid.UUID
	if req.CategoryID != nil {
		if *req.CategoryID != "" {
			parsed, err := uuid.Parse(*req.CategoryID)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid category ID"})
				return
			}
			categoryID = &parsed
		}
	}

	var tagsJSON, keywordsJSON []byte
	if req.Tags != nil {
		tagsJSON, _ = json.Marshal(req.Tags)
	}
	if req.Keywords != nil {
		keywordsJSON, _ = json.Marshal(req.Keywords)
	}

	// Check if status is changing to published
	var currentStatus string
	r.db.Pool().QueryRow(ctx, "SELECT status FROM kb_articles WHERE id = $1", id).Scan(&currentStatus)

	var publishedAt *time.Time
	if req.Status == "published" && currentStatus != "published" {
		now := time.Now()
		publishedAt = &now
	}

	var lastReviewedAt *time.Time
	if req.LastReviewedBy != "" {
		now := time.Now()
		lastReviewedAt = &now
	}

	_, err = r.db.Pool().Exec(ctx, `
		UPDATE kb_articles SET
			title = COALESCE(NULLIF($1, ''), title),
			slug = COALESCE(NULLIF($2, ''), slug),
			content = COALESCE(NULLIF($3, ''), content),
			content_html = COALESCE(NULLIF($4, ''), content_html),
			summary = COALESCE(NULLIF($5, ''), summary),
			category_id = $6,
			status = COALESCE(NULLIF($7, ''), status),
			is_featured = COALESCE($8, is_featured),
			is_pinned = COALESCE($9, is_pinned),
			tags = COALESCE($10::jsonb, tags),
			keywords = COALESCE($11::jsonb, keywords),
			published_at = COALESCE($12, published_at),
			last_reviewed_at = COALESCE($13, last_reviewed_at),
			last_reviewed_by = COALESCE(NULLIF($14, ''), last_reviewed_by),
			updated_at = NOW()
		WHERE id = $15
	`, req.Title, req.Slug, req.Content, req.ContentHTML, req.Summary, categoryID, req.Status,
		req.IsFeatured, req.IsPinned, tagsJSON, keywordsJSON, publishedAt, lastReviewedAt, req.LastReviewedBy, id)

	if err != nil {
		log.Printf("Error updating KB article: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update article"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Article updated successfully"})
}

func (r *Router) deleteKBArticle(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid article ID"})
		return
	}

	ctx := context.Background()
	result, err := r.db.Pool().Exec(ctx, "DELETE FROM kb_articles WHERE id = $1", id)
	if err != nil {
		log.Printf("Error deleting KB article: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete article"})
		return
	}

	if result.RowsAffected() == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "Article not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Article deleted successfully"})
}

// KB Article Feedback handlers

func (r *Router) submitKBArticleFeedback(c *gin.Context) {
	articleID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid article ID"})
		return
	}

	var req struct {
		IsHelpful bool   `json:"isHelpful"`
		Comment   string `json:"comment"`
		UserEmail string `json:"userEmail"`
		UserName  string `json:"userName"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx := context.Background()
	ipAddress := c.ClientIP()
	userAgent := c.Request.UserAgent()

	var id uuid.UUID
	err = r.db.Pool().QueryRow(ctx, `
		INSERT INTO kb_article_feedback (article_id, is_helpful, comment, user_email, user_name, ip_address, user_agent)
		VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id
	`, articleID, req.IsHelpful, req.Comment, req.UserEmail, req.UserName, ipAddress, userAgent).Scan(&id)

	if err != nil {
		log.Printf("Error submitting KB feedback: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to submit feedback"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"id": id, "message": "Feedback submitted successfully"})
}

func (r *Router) getKBArticleFeedback(c *gin.Context) {
	articleID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid article ID"})
		return
	}

	ctx := context.Background()
	rows, err := r.db.Pool().Query(ctx, `
		SELECT id, is_helpful, comment, user_email, user_name, created_at
		FROM kb_article_feedback WHERE article_id = $1 ORDER BY created_at DESC
	`, articleID)
	if err != nil {
		log.Printf("Error getting KB feedback: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch feedback"})
		return
	}
	defer rows.Close()

	feedback := make([]map[string]interface{}, 0)
	for rows.Next() {
		var id uuid.UUID
		var isHelpful bool
		var comment, userEmail, userName *string
		var createdAt time.Time

		if err := rows.Scan(&id, &isHelpful, &comment, &userEmail, &userName, &createdAt); err != nil {
			continue
		}
		feedback = append(feedback, map[string]interface{}{
			"id":        id,
			"isHelpful": isHelpful,
			"comment":   comment,
			"userEmail": userEmail,
			"userName":  userName,
			"createdAt": createdAt,
		})
	}

	c.JSON(http.StatusOK, feedback)
}

// KB Article View tracking

func (r *Router) recordKBArticleView(c *gin.Context) {
	articleID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid article ID"})
		return
	}

	var req struct {
		UserEmail string `json:"userEmail"`
		SessionID string `json:"sessionId"`
		Referrer  string `json:"referrer"`
	}

	c.ShouldBindJSON(&req)

	ctx := context.Background()
	ipAddress := c.ClientIP()

	// Record view
	r.db.Pool().Exec(ctx, `
		INSERT INTO kb_article_views (article_id, user_email, session_id, ip_address, referrer)
		VALUES ($1, $2, $3, $4, $5)
	`, articleID, req.UserEmail, req.SessionID, ipAddress, req.Referrer)

	// Increment view count
	r.db.Pool().Exec(ctx, "UPDATE kb_articles SET view_count = view_count + 1 WHERE id = $1", articleID)

	c.JSON(http.StatusOK, gin.H{"message": "View recorded"})
}

// Handler wrappers for KB categories
func listKBCategoriesHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis}
	return router.listKBCategories
}

func getKBCategoryHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis}
	return router.getKBCategory
}

func createKBCategoryHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis}
	return router.createKBCategory
}

func updateKBCategoryHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis}
	return router.updateKBCategory
}

func deleteKBCategoryHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis}
	return router.deleteKBCategory
}

// Handler wrappers for KB articles
func listKBArticlesHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis}
	return router.listKBArticles
}

func getKBArticleHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis}
	return router.getKBArticle
}

func createKBArticleHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis}
	return router.createKBArticle
}

func updateKBArticleHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis}
	return router.updateKBArticle
}

func deleteKBArticleHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis}
	return router.deleteKBArticle
}

func submitKBArticleFeedbackHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis}
	return router.submitKBArticleFeedback
}

func getKBArticleFeedbackHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis}
	return router.getKBArticleFeedback
}

func recordKBArticleViewHandler(services *Services) gin.HandlerFunc {
	router := &Router{config: services.Config, db: services.DB.AsDB(), cache: services.Redis}
	return router.recordKBArticleView
}
