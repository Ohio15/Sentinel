import React, { useState, useEffect } from 'react';
import {
  Plus,
  Search,
  Edit,
  Trash2,
  Eye,
  Star,
  Pin,
  Folder,
  FileText,
  MoreVertical,
  ChevronRight,
  Clock,
  ThumbsUp,
  ThumbsDown,
  ExternalLink
} from 'lucide-react';

interface KBCategory {
  id: string;
  name: string;
  slug: string;
  description: string | null;
  parentId: string | null;
  icon: string;
  color: string;
  sortOrder: number;
  isActive: boolean;
  articleCount: number;
}

interface KBArticle {
  id: string;
  title: string;
  slug: string;
  content: string | null;
  contentHtml: string | null;
  summary: string | null;
  categoryId: string | null;
  categoryName?: string;
  status: 'draft' | 'published' | 'archived';
  isFeatured: boolean;
  isPinned: boolean;
  tags: string[];
  viewCount: number;
  helpfulCount: number;
  notHelpfulCount: number;
  authorName: string | null;
  publishedAt: string | null;
  createdAt: string;
  updatedAt: string;
}

export function KnowledgeBase() {
  const [categories, setCategories] = useState<KBCategory[]>([]);
  const [articles, setArticles] = useState<KBArticle[]>([]);
  const [loading, setLoading] = useState(true);
  const [searchTerm, setSearchTerm] = useState('');
  const [selectedCategoryId, setSelectedCategoryId] = useState<string | null>(null);
  const [statusFilter, setStatusFilter] = useState<string>('');
  const [showArticleModal, setShowArticleModal] = useState(false);
  const [showCategoryModal, setShowCategoryModal] = useState(false);
  const [editingArticle, setEditingArticle] = useState<KBArticle | null>(null);
  const [editingCategory, setEditingCategory] = useState<KBCategory | null>(null);

  useEffect(() => {
    loadData();
  }, []);

  const loadData = async () => {
    setLoading(true);
    try {
      const [cats, arts] = await Promise.all([
        window.api.kb.categories.list(),
        window.api.kb.articles.list()
      ]);
      setCategories(cats);
      setArticles(arts);
    } catch (error) {
      console.error('Failed to load KB data:', error);
    } finally {
      setLoading(false);
    }
  };

  const filteredArticles = articles.filter((article) => {
    const matchesSearch = searchTerm
      ? article.title.toLowerCase().includes(searchTerm.toLowerCase()) ||
        article.summary?.toLowerCase().includes(searchTerm.toLowerCase())
      : true;

    const matchesCategory = selectedCategoryId
      ? article.categoryId === selectedCategoryId
      : true;

    const matchesStatus = statusFilter
      ? article.status === statusFilter
      : true;

    return matchesSearch && matchesCategory && matchesStatus;
  });

  const handleCreateArticle = async (data: Partial<KBArticle>) => {
    try {
      await window.api.kb.articles.create(data);
      await loadData();
      setShowArticleModal(false);
      setEditingArticle(null);
    } catch (error) {
      console.error('Failed to create article:', error);
    }
  };

  const handleUpdateArticle = async (id: string, data: Partial<KBArticle>) => {
    try {
      await window.api.kb.articles.update(id, data);
      await loadData();
      setShowArticleModal(false);
      setEditingArticle(null);
    } catch (error) {
      console.error('Failed to update article:', error);
    }
  };

  const handleDeleteArticle = async (id: string) => {
    if (!confirm('Are you sure you want to delete this article?')) return;
    try {
      await window.api.kb.articles.delete(id);
      await loadData();
    } catch (error) {
      console.error('Failed to delete article:', error);
    }
  };

  const handleToggleFeatured = async (article: KBArticle) => {
    try {
      await window.api.kb.articles.update(article.id, { isFeatured: !article.isFeatured });
      await loadData();
    } catch (error) {
      console.error('Failed to toggle featured:', error);
    }
  };

  const handleTogglePinned = async (article: KBArticle) => {
    try {
      await window.api.kb.articles.update(article.id, { isPinned: !article.isPinned });
      await loadData();
    } catch (error) {
      console.error('Failed to toggle pinned:', error);
    }
  };

  const handlePublishArticle = async (article: KBArticle) => {
    try {
      await window.api.kb.articles.update(article.id, {
        status: 'published',
        publishedAt: new Date().toISOString()
      });
      await loadData();
    } catch (error) {
      console.error('Failed to publish article:', error);
    }
  };

  const formatDate = (dateString: string) => {
    return new Date(dateString).toLocaleDateString();
  };

  const getStatusBadge = (status: string) => {
    switch (status) {
      case 'published':
        return 'bg-green-100 text-green-700 dark:bg-green-900/30 dark:text-green-400';
      case 'draft':
        return 'bg-yellow-100 text-yellow-700 dark:bg-yellow-900/30 dark:text-yellow-400';
      case 'archived':
        return 'bg-gray-100 text-gray-700 dark:bg-gray-700 dark:text-gray-400';
      default:
        return 'bg-gray-100 text-gray-700';
    }
  };

  if (loading) {
    return (
      <div className="flex items-center justify-center h-full">
        <p className="text-text-secondary">Loading knowledge base...</p>
      </div>
    );
  }

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold text-text-primary">Knowledge Base</h1>
        <div className="flex items-center gap-3">
          <button
            onClick={() => {
              setEditingCategory(null);
              setShowCategoryModal(true);
            }}
            className="btn btn-secondary flex items-center gap-2"
          >
            <Folder className="w-4 h-4" />
            Add Category
          </button>
          <button
            onClick={() => {
              setEditingArticle(null);
              setShowArticleModal(true);
            }}
            className="btn btn-primary flex items-center gap-2"
          >
            <Plus className="w-5 h-5" />
            New Article
          </button>
        </div>
      </div>

      {/* Stats */}
      <div className="grid grid-cols-2 md:grid-cols-5 gap-4">
        <StatCard
          label="Total Articles"
          value={articles.length}
          icon={<FileText className="w-5 h-5 text-blue-500" />}
        />
        <StatCard
          label="Published"
          value={articles.filter((a) => a.status === 'published').length}
          icon={<Eye className="w-5 h-5 text-green-500" />}
        />
        <StatCard
          label="Drafts"
          value={articles.filter((a) => a.status === 'draft').length}
          icon={<Edit className="w-5 h-5 text-yellow-500" />}
        />
        <StatCard
          label="Featured"
          value={articles.filter((a) => a.isFeatured).length}
          icon={<Star className="w-5 h-5 text-orange-500" />}
        />
        <StatCard
          label="Categories"
          value={categories.length}
          icon={<Folder className="w-5 h-5 text-purple-500" />}
        />
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-4 gap-6">
        {/* Categories Sidebar */}
        <div className="card p-4">
          <h3 className="font-semibold text-text-primary mb-3">Categories</h3>
          <div className="space-y-1">
            <button
              onClick={() => setSelectedCategoryId(null)}
              className={`w-full flex items-center justify-between px-3 py-2 rounded-lg text-left ${
                selectedCategoryId === null
                  ? 'bg-blue-50 dark:bg-blue-900/30 text-blue-700 dark:text-blue-400'
                  : 'hover:bg-gray-100 dark:hover:bg-gray-700 text-text-primary'
              }`}
            >
              <span>All Articles</span>
              <span className="text-sm text-text-secondary">{articles.length}</span>
            </button>
            {categories.map((cat) => (
              <button
                key={cat.id}
                onClick={() => setSelectedCategoryId(cat.id)}
                className={`w-full flex items-center justify-between px-3 py-2 rounded-lg text-left ${
                  selectedCategoryId === cat.id
                    ? 'bg-blue-50 dark:bg-blue-900/30 text-blue-700 dark:text-blue-400'
                    : 'hover:bg-gray-100 dark:hover:bg-gray-700 text-text-primary'
                }`}
              >
                <div className="flex items-center gap-2">
                  <span
                    className="w-3 h-3 rounded-full"
                    style={{ backgroundColor: cat.color }}
                  />
                  <span>{cat.name}</span>
                </div>
                <span className="text-sm text-text-secondary">{cat.articleCount}</span>
              </button>
            ))}
          </div>
        </div>

        {/* Articles List */}
        <div className="lg:col-span-3 space-y-4">
          {/* Filters */}
          <div className="card p-4">
            <div className="flex flex-wrap items-center gap-4">
              <div className="flex-1 min-w-[200px]">
                <div className="relative">
                  <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-5 h-5 text-text-secondary" />
                  <input
                    type="text"
                    placeholder="Search articles..."
                    value={searchTerm}
                    onChange={(e) => setSearchTerm(e.target.value)}
                    className="input pl-10"
                  />
                </div>
              </div>
              <select
                value={statusFilter}
                onChange={(e) => setStatusFilter(e.target.value)}
                className="input w-auto"
              >
                <option value="">All Statuses</option>
                <option value="published">Published</option>
                <option value="draft">Draft</option>
                <option value="archived">Archived</option>
              </select>
            </div>
          </div>

          {/* Articles Table */}
          <div className="card overflow-hidden">
            {filteredArticles.length === 0 ? (
              <div className="p-8 text-center text-text-secondary">
                {articles.length === 0
                  ? 'No articles yet. Create your first article!'
                  : 'No articles match your filters.'}
              </div>
            ) : (
              <table className="w-full">
                <thead>
                  <tr className="bg-gray-50 dark:bg-gray-800">
                    <th className="text-left px-4 py-3 text-text-secondary font-medium">Article</th>
                    <th className="text-left px-4 py-3 text-text-secondary font-medium">Category</th>
                    <th className="text-left px-4 py-3 text-text-secondary font-medium">Status</th>
                    <th className="text-center px-4 py-3 text-text-secondary font-medium">Views</th>
                    <th className="text-center px-4 py-3 text-text-secondary font-medium">Feedback</th>
                    <th className="text-left px-4 py-3 text-text-secondary font-medium">Updated</th>
                    <th className="text-right px-4 py-3 text-text-secondary font-medium">Actions</th>
                  </tr>
                </thead>
                <tbody>
                  {filteredArticles.map((article) => (
                    <tr
                      key={article.id}
                      className="border-t border-gray-100 dark:border-gray-700 hover:bg-gray-50 dark:hover:bg-gray-800"
                    >
                      <td className="px-4 py-3">
                        <div className="flex items-center gap-2">
                          {article.isPinned && (
                            <Pin className="w-4 h-4 text-blue-500 flex-shrink-0" />
                          )}
                          {article.isFeatured && (
                            <Star className="w-4 h-4 text-yellow-500 flex-shrink-0 fill-yellow-500" />
                          )}
                          <div>
                            <div className="font-medium text-text-primary">{article.title}</div>
                            {article.summary && (
                              <div className="text-sm text-text-secondary truncate max-w-md">
                                {article.summary}
                              </div>
                            )}
                          </div>
                        </div>
                      </td>
                      <td className="px-4 py-3 text-text-primary">
                        {article.categoryName || '-'}
                      </td>
                      <td className="px-4 py-3">
                        <span className={`px-2 py-0.5 rounded-full text-xs font-medium ${getStatusBadge(article.status)}`}>
                          {article.status}
                        </span>
                      </td>
                      <td className="px-4 py-3 text-center text-text-secondary">
                        {article.viewCount}
                      </td>
                      <td className="px-4 py-3">
                        <div className="flex items-center justify-center gap-3 text-sm">
                          <span className="flex items-center gap-1 text-green-600">
                            <ThumbsUp className="w-3 h-3" />
                            {article.helpfulCount}
                          </span>
                          <span className="flex items-center gap-1 text-red-500">
                            <ThumbsDown className="w-3 h-3" />
                            {article.notHelpfulCount}
                          </span>
                        </div>
                      </td>
                      <td className="px-4 py-3 text-text-secondary text-sm">
                        {formatDate(article.updatedAt)}
                      </td>
                      <td className="px-4 py-3">
                        <div className="flex items-center justify-end gap-2">
                          {article.status === 'draft' && (
                            <button
                              onClick={() => handlePublishArticle(article)}
                              className="p-1.5 text-green-600 hover:bg-green-50 dark:hover:bg-green-900/30 rounded"
                              title="Publish"
                            >
                              <Eye className="w-4 h-4" />
                            </button>
                          )}
                          <button
                            onClick={() => handleToggleFeatured(article)}
                            className={`p-1.5 rounded ${article.isFeatured ? 'text-yellow-500 bg-yellow-50 dark:bg-yellow-900/30' : 'text-gray-400 hover:bg-gray-100 dark:hover:bg-gray-700'}`}
                            title={article.isFeatured ? 'Unfeature' : 'Feature'}
                          >
                            <Star className={`w-4 h-4 ${article.isFeatured ? 'fill-yellow-500' : ''}`} />
                          </button>
                          <button
                            onClick={() => handleTogglePinned(article)}
                            className={`p-1.5 rounded ${article.isPinned ? 'text-blue-500 bg-blue-50 dark:bg-blue-900/30' : 'text-gray-400 hover:bg-gray-100 dark:hover:bg-gray-700'}`}
                            title={article.isPinned ? 'Unpin' : 'Pin'}
                          >
                            <Pin className="w-4 h-4" />
                          </button>
                          <button
                            onClick={() => {
                              setEditingArticle(article);
                              setShowArticleModal(true);
                            }}
                            className="p-1.5 text-gray-400 hover:text-blue-600 hover:bg-blue-50 dark:hover:bg-blue-900/30 rounded"
                            title="Edit"
                          >
                            <Edit className="w-4 h-4" />
                          </button>
                          <button
                            onClick={() => handleDeleteArticle(article.id)}
                            className="p-1.5 text-gray-400 hover:text-red-600 hover:bg-red-50 dark:hover:bg-red-900/30 rounded"
                            title="Delete"
                          >
                            <Trash2 className="w-4 h-4" />
                          </button>
                        </div>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
          </div>
        </div>
      </div>

      {/* Article Modal */}
      {showArticleModal && (
        <ArticleModal
          article={editingArticle}
          categories={categories}
          onClose={() => {
            setShowArticleModal(false);
            setEditingArticle(null);
          }}
          onSave={(data) => {
            if (editingArticle) {
              handleUpdateArticle(editingArticle.id, data);
            } else {
              handleCreateArticle(data);
            }
          }}
        />
      )}

      {/* Category Modal */}
      {showCategoryModal && (
        <CategoryModal
          category={editingCategory}
          onClose={() => {
            setShowCategoryModal(false);
            setEditingCategory(null);
          }}
          onSave={async (data) => {
            try {
              if (editingCategory) {
                await window.api.kb.categories.update(editingCategory.id, data);
              } else {
                await window.api.kb.categories.create(data);
              }
              await loadData();
              setShowCategoryModal(false);
              setEditingCategory(null);
            } catch (error) {
              console.error('Failed to save category:', error);
            }
          }}
        />
      )}
    </div>
  );
}

// Stat Card Component
function StatCard({
  label,
  value,
  icon
}: {
  label: string;
  value: number;
  icon: React.ReactNode;
}) {
  return (
    <div className="card p-4">
      <div className="flex items-center justify-between">
        <div>
          <p className="text-sm text-text-secondary">{label}</p>
          <p className="text-2xl font-bold text-text-primary mt-1">{value}</p>
        </div>
        <div className="opacity-50">{icon}</div>
      </div>
    </div>
  );
}

// Article Modal
function ArticleModal({
  article,
  categories,
  onClose,
  onSave
}: {
  article: KBArticle | null;
  categories: KBCategory[];
  onClose: () => void;
  onSave: (data: Partial<KBArticle>) => void;
}) {
  const [formData, setFormData] = useState({
    title: article?.title || '',
    summary: article?.summary || '',
    content: article?.content || '',
    categoryId: article?.categoryId || '',
    status: article?.status || 'draft',
    isFeatured: article?.isFeatured || false,
    isPinned: article?.isPinned || false,
    tags: article?.tags?.join(', ') || '',
    authorName: article?.authorName || ''
  });
  const [saving, setSaving] = useState(false);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!formData.title.trim()) return;

    setSaving(true);
    try {
      onSave({
        ...formData,
        categoryId: formData.categoryId || undefined,
        tags: formData.tags
          .split(',')
          .map((t) => t.trim())
          .filter((t) => t)
      });
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50">
      <div className="bg-surface rounded-lg shadow-xl w-full max-w-4xl max-h-[90vh] overflow-auto">
        <div className="p-6 border-b border-border">
          <h2 className="text-xl font-semibold text-text-primary">
            {article ? 'Edit Article' : 'New Article'}
          </h2>
        </div>
        <form onSubmit={handleSubmit} className="p-6 space-y-4">
          <div className="grid grid-cols-2 gap-4">
            <div className="col-span-2">
              <label className="label">Title *</label>
              <input
                type="text"
                value={formData.title}
                onChange={(e) => setFormData({ ...formData, title: e.target.value })}
                className="input"
                placeholder="Article title"
                required
              />
            </div>
            <div>
              <label className="label">Category</label>
              <select
                value={formData.categoryId}
                onChange={(e) => setFormData({ ...formData, categoryId: e.target.value })}
                className="input"
              >
                <option value="">No category</option>
                {categories.map((cat) => (
                  <option key={cat.id} value={cat.id}>
                    {cat.name}
                  </option>
                ))}
              </select>
            </div>
            <div>
              <label className="label">Status</label>
              <select
                value={formData.status}
                onChange={(e) => setFormData({ ...formData, status: e.target.value as any })}
                className="input"
              >
                <option value="draft">Draft</option>
                <option value="published">Published</option>
                <option value="archived">Archived</option>
              </select>
            </div>
          </div>

          <div>
            <label className="label">Summary</label>
            <textarea
              value={formData.summary}
              onChange={(e) => setFormData({ ...formData, summary: e.target.value })}
              className="input min-h-[60px]"
              placeholder="Brief description for search results..."
            />
          </div>

          <div>
            <label className="label">Content (Markdown)</label>
            <textarea
              value={formData.content}
              onChange={(e) => setFormData({ ...formData, content: e.target.value })}
              className="input min-h-[300px] font-mono text-sm"
              placeholder="Write your article content in Markdown..."
            />
          </div>

          <div className="grid grid-cols-2 gap-4">
            <div>
              <label className="label">Tags</label>
              <input
                type="text"
                value={formData.tags}
                onChange={(e) => setFormData({ ...formData, tags: e.target.value })}
                className="input"
                placeholder="vpn, security, network (comma-separated)"
              />
            </div>
            <div>
              <label className="label">Author Name</label>
              <input
                type="text"
                value={formData.authorName}
                onChange={(e) => setFormData({ ...formData, authorName: e.target.value })}
                className="input"
                placeholder="Author name"
              />
            </div>
          </div>

          <div className="flex items-center gap-6">
            <label className="flex items-center gap-2">
              <input
                type="checkbox"
                checked={formData.isFeatured}
                onChange={(e) => setFormData({ ...formData, isFeatured: e.target.checked })}
                className="w-4 h-4"
              />
              <span className="text-text-primary">Featured article</span>
            </label>
            <label className="flex items-center gap-2">
              <input
                type="checkbox"
                checked={formData.isPinned}
                onChange={(e) => setFormData({ ...formData, isPinned: e.target.checked })}
                className="w-4 h-4"
              />
              <span className="text-text-primary">Pinned article</span>
            </label>
          </div>

          <div className="flex justify-end gap-3 pt-4 border-t border-border">
            <button type="button" onClick={onClose} className="btn btn-secondary">
              Cancel
            </button>
            <button type="submit" disabled={saving} className="btn btn-primary">
              {saving ? 'Saving...' : article ? 'Update Article' : 'Create Article'}
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}

// Category Modal
function CategoryModal({
  category,
  onClose,
  onSave
}: {
  category: KBCategory | null;
  onClose: () => void;
  onSave: (data: Partial<KBCategory>) => void;
}) {
  const [formData, setFormData] = useState({
    name: category?.name || '',
    description: category?.description || '',
    color: category?.color || '#6B7280',
    icon: category?.icon || 'folder',
    sortOrder: category?.sortOrder || 0,
    isActive: category?.isActive ?? true
  });
  const [saving, setSaving] = useState(false);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!formData.name.trim()) return;

    setSaving(true);
    try {
      onSave(formData);
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50">
      <div className="bg-surface rounded-lg shadow-xl w-full max-w-md">
        <div className="p-6 border-b border-border">
          <h2 className="text-xl font-semibold text-text-primary">
            {category ? 'Edit Category' : 'New Category'}
          </h2>
        </div>
        <form onSubmit={handleSubmit} className="p-6 space-y-4">
          <div>
            <label className="label">Name *</label>
            <input
              type="text"
              value={formData.name}
              onChange={(e) => setFormData({ ...formData, name: e.target.value })}
              className="input"
              placeholder="Category name"
              required
            />
          </div>
          <div>
            <label className="label">Description</label>
            <textarea
              value={formData.description}
              onChange={(e) => setFormData({ ...formData, description: e.target.value })}
              className="input min-h-[80px]"
              placeholder="Category description..."
            />
          </div>
          <div className="grid grid-cols-2 gap-4">
            <div>
              <label className="label">Color</label>
              <div className="flex items-center gap-2">
                <input
                  type="color"
                  value={formData.color}
                  onChange={(e) => setFormData({ ...formData, color: e.target.value })}
                  className="w-10 h-10 rounded cursor-pointer"
                />
                <input
                  type="text"
                  value={formData.color}
                  onChange={(e) => setFormData({ ...formData, color: e.target.value })}
                  className="input flex-1"
                />
              </div>
            </div>
            <div>
              <label className="label">Sort Order</label>
              <input
                type="number"
                value={formData.sortOrder}
                onChange={(e) => setFormData({ ...formData, sortOrder: parseInt(e.target.value) || 0 })}
                className="input"
              />
            </div>
          </div>
          <label className="flex items-center gap-2">
            <input
              type="checkbox"
              checked={formData.isActive}
              onChange={(e) => setFormData({ ...formData, isActive: e.target.checked })}
              className="w-4 h-4"
            />
            <span className="text-text-primary">Active</span>
          </label>

          <div className="flex justify-end gap-3 pt-4 border-t border-border">
            <button type="button" onClick={onClose} className="btn btn-secondary">
              Cancel
            </button>
            <button type="submit" disabled={saving} className="btn btn-primary">
              {saving ? 'Saving...' : category ? 'Update Category' : 'Create Category'}
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}

export default KnowledgeBase;
