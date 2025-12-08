import { useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import {
  FileCode,
  Plus,
  Search,
  Edit,
  Trash2,
  Copy,
} from 'lucide-react';
import { Header } from '@/components/layout';
import { Card, CardContent, Badge, Button, Input, Modal } from '@/components/ui';
import api from '@/services/api';
import type { Script } from '@/types';
import toast from 'react-hot-toast';

export function Scripts() {
  const queryClient = useQueryClient();
  const [searchQuery, setSearchQuery] = useState('');
  const [languageFilter, setLanguageFilter] = useState<string>('all');
  const [showCreateModal, setShowCreateModal] = useState(false);
  const [editingScript, setEditingScript] = useState<Script | null>(null);
  const [scriptForm, setScriptForm] = useState({
    name: '',
    description: '',
    language: 'powershell' as 'powershell' | 'bash' | 'python',
    content: '',
    osTypes: ['windows'] as string[],
  });

  const { data, isLoading } = useQuery({
    queryKey: ['scripts', languageFilter, searchQuery],
    queryFn: () =>
      api.getScripts({
        language: languageFilter !== 'all' ? languageFilter : undefined,
        search: searchQuery || undefined,
        pageSize: 100,
      }),
  });

  // API returns array directly, not { scripts: [...] }
  const scripts: Script[] = Array.isArray(data) ? data : (data?.scripts || []);

  const createMutation = useMutation({
    mutationFn: (data: typeof scriptForm) => api.createScript(data),
    onSuccess: () => {
      toast.success('Script created successfully');
      queryClient.invalidateQueries({ queryKey: ['scripts'] });
      setShowCreateModal(false);
      resetForm();
    },
    onError: () => {
      toast.error('Failed to create script');
    },
  });

  const updateMutation = useMutation({
    mutationFn: ({ id, data }: { id: string; data: Partial<typeof scriptForm> }) =>
      api.updateScript(id, data),
    onSuccess: () => {
      toast.success('Script updated successfully');
      queryClient.invalidateQueries({ queryKey: ['scripts'] });
      setEditingScript(null);
      resetForm();
    },
    onError: () => {
      toast.error('Failed to update script');
    },
  });

  const deleteMutation = useMutation({
    mutationFn: (id: string) => api.deleteScript(id),
    onSuccess: () => {
      toast.success('Script deleted successfully');
      queryClient.invalidateQueries({ queryKey: ['scripts'] });
    },
    onError: () => {
      toast.error('Failed to delete script');
    },
  });

  const resetForm = () => {
    setScriptForm({
      name: '',
      description: '',
      language: 'powershell' as 'powershell' | 'bash' | 'python',
      content: '',
      osTypes: ['windows'],
    });
  };

  const handleEdit = (script: Script) => {
    setEditingScript(script);
    setScriptForm({
      name: script.name,
      description: script.description || '',
      language: script.language,
      content: script.content,
      osTypes: script.osTypes,
    });
  };

  const handleSubmit = () => {
    if (editingScript) {
      updateMutation.mutate({ id: editingScript.id, data: scriptForm });
    } else {
      createMutation.mutate(scriptForm);
    }
  };

  const getLanguageColor = (language: string) => {
    switch (language) {
      case 'powershell':
        return 'bg-blue-100 text-blue-700';
      case 'bash':
        return 'bg-green-100 text-green-700';
      case 'python':
        return 'bg-yellow-100 text-yellow-700';
      default:
        return 'bg-gray-100 text-gray-700';
    }
  };

  return (
    <div>
      <Header
        title="Scripts"
        subtitle={`${scripts.length} scripts in library`}
      />

      <div className="p-6 space-y-6">
        {/* Filters and Actions */}
        <div className="flex flex-col sm:flex-row gap-4 justify-between">
          <div className="flex gap-4 flex-1">
            <div className="relative flex-1 max-w-md">
              <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-text-secondary" />
              <input
                type="text"
                placeholder="Search scripts..."
                value={searchQuery}
                onChange={(e) => setSearchQuery(e.target.value)}
                className="w-full pl-10 pr-4 py-2 bg-surface border border-border rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-primary focus:border-transparent"
              />
            </div>

            <div className="flex gap-2">
              {(['all', 'powershell', 'bash', 'python'] as const).map((lang) => (
                <button
                  key={lang}
                  onClick={() => setLanguageFilter(lang)}
                  className={`px-3 py-2 text-sm font-medium rounded-lg transition-colors ${
                    languageFilter === lang
                      ? 'bg-primary text-white'
                      : 'bg-surface border border-border text-text-secondary hover:text-text-primary'
                  }`}
                >
                  {lang.charAt(0).toUpperCase() + lang.slice(1)}
                </button>
              ))}
            </div>
          </div>

          <Button onClick={() => setShowCreateModal(true)}>
            <Plus className="w-4 h-4 mr-2" />
            New Script
          </Button>
        </div>

        {/* Scripts Grid */}
        {isLoading ? (
          <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
            {[...Array(6)].map((_, i) => (
              <Card key={i}>
                <CardContent>
                  <div className="animate-pulse space-y-3">
                    <div className="h-5 bg-gray-200 rounded w-3/4" />
                    <div className="h-4 bg-gray-200 rounded w-full" />
                    <div className="h-4 bg-gray-200 rounded w-1/2" />
                  </div>
                </CardContent>
              </Card>
            ))}
          </div>
        ) : scripts.length > 0 ? (
          <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
            {scripts.map((script) => (
              <Card key={script.id} className="hover:border-primary/30 transition-colors">
                <CardContent>
                  <div className="flex items-start justify-between mb-3">
                    <div className="flex items-center gap-2">
                      <FileCode className="w-5 h-5 text-text-secondary" />
                      <h3 className="font-medium text-text-primary">
                        {script.name}
                      </h3>
                    </div>
                    <span
                      className={`px-2 py-0.5 text-xs font-medium rounded ${getLanguageColor(
                        script.language
                      )}`}
                    >
                      {script.language}
                    </span>
                  </div>

                  {script.description && (
                    <p className="text-sm text-text-secondary mb-3 line-clamp-2">
                      {script.description}
                    </p>
                  )}

                  <div className="flex items-center gap-2 mb-4">
                    {script.osTypes.map((os) => (
                      <Badge key={os} variant="default" size="sm">
                        {os}
                      </Badge>
                    ))}
                    {script.isSystem && (
                      <Badge variant="info" size="sm">
                        System
                      </Badge>
                    )}
                  </div>

                  <div className="flex items-center gap-2 pt-3 border-t border-border">
                    <Button
                      size="sm"
                      variant="secondary"
                      onClick={() => handleEdit(script)}
                      disabled={script.isSystem}
                    >
                      <Edit className="w-3.5 h-3.5" />
                    </Button>
                    <Button
                      size="sm"
                      variant="secondary"
                      onClick={() => {
                        navigator.clipboard.writeText(script.content);
                        toast.success('Script copied to clipboard');
                      }}
                    >
                      <Copy className="w-3.5 h-3.5" />
                    </Button>
                    <Button
                      size="sm"
                      variant="danger"
                      onClick={() => {
                        if (confirm('Delete this script?')) {
                          deleteMutation.mutate(script.id);
                        }
                      }}
                      disabled={script.isSystem}
                    >
                      <Trash2 className="w-3.5 h-3.5" />
                    </Button>
                  </div>
                </CardContent>
              </Card>
            ))}
          </div>
        ) : (
          <Card>
            <CardContent>
              <div className="text-center py-12">
                <FileCode className="w-12 h-12 text-text-secondary mx-auto mb-4" />
                <h3 className="text-lg font-medium text-text-primary mb-2">
                  No scripts found
                </h3>
                <p className="text-text-secondary mb-4">
                  {searchQuery || languageFilter !== 'all'
                    ? 'Try adjusting your filters'
                    : 'Create your first script to get started'}
                </p>
                <Button onClick={() => setShowCreateModal(true)}>
                  <Plus className="w-4 h-4 mr-2" />
                  Create Script
                </Button>
              </div>
            </CardContent>
          </Card>
        )}
      </div>

      {/* Create/Edit Modal */}
      <Modal
        isOpen={showCreateModal || !!editingScript}
        onClose={() => {
          setShowCreateModal(false);
          setEditingScript(null);
          resetForm();
        }}
        title={editingScript ? 'Edit Script' : 'Create Script'}
        size="xl"
      >
        <div className="space-y-4">
          <Input
            label="Name"
            value={scriptForm.name}
            onChange={(e) =>
              setScriptForm({ ...scriptForm, name: e.target.value })
            }
            placeholder="My Script"
            required
          />

          <Input
            label="Description"
            value={scriptForm.description}
            onChange={(e) =>
              setScriptForm({ ...scriptForm, description: e.target.value })
            }
            placeholder="What does this script do?"
          />

          <div>
            <label className="block text-sm font-medium text-text-primary mb-1.5">
              Language
            </label>
            <select
              value={scriptForm.language}
              onChange={(e) =>
                setScriptForm({
                  ...scriptForm,
                  language: e.target.value as 'powershell' | 'bash' | 'python',
                })
              }
              className="w-full px-3 py-2 border border-border rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-primary"
            >
              <option value="powershell">PowerShell</option>
              <option value="bash">Bash</option>
              <option value="python">Python</option>
            </select>
          </div>

          <div>
            <label className="block text-sm font-medium text-text-primary mb-1.5">
              Target OS
            </label>
            <div className="flex gap-2">
              {['windows', 'linux', 'macos'].map((os) => (
                <label key={os} className="flex items-center gap-2">
                  <input
                    type="checkbox"
                    checked={scriptForm.osTypes.includes(os)}
                    onChange={(e) => {
                      if (e.target.checked) {
                        setScriptForm({
                          ...scriptForm,
                          osTypes: [...scriptForm.osTypes, os],
                        });
                      } else {
                        setScriptForm({
                          ...scriptForm,
                          osTypes: scriptForm.osTypes.filter((t) => t !== os),
                        });
                      }
                    }}
                    className="rounded border-border"
                  />
                  <span className="text-sm capitalize">{os}</span>
                </label>
              ))}
            </div>
          </div>

          <div>
            <label className="block text-sm font-medium text-text-primary mb-1.5">
              Script Content
            </label>
            <textarea
              value={scriptForm.content}
              onChange={(e) =>
                setScriptForm({ ...scriptForm, content: e.target.value })
              }
              placeholder="Enter your script here..."
              className="w-full h-64 px-3 py-2 border border-border rounded-lg font-mono text-sm focus:outline-none focus:ring-2 focus:ring-primary resize-none"
            />
          </div>

          <div className="flex gap-3 justify-end pt-4">
            <Button
              variant="secondary"
              onClick={() => {
                setShowCreateModal(false);
                setEditingScript(null);
                resetForm();
              }}
            >
              Cancel
            </Button>
            <Button
              onClick={handleSubmit}
              disabled={
                !scriptForm.name ||
                !scriptForm.content ||
                scriptForm.osTypes.length === 0
              }
              isLoading={createMutation.isPending || updateMutation.isPending}
            >
              {editingScript ? 'Update' : 'Create'} Script
            </Button>
          </div>
        </div>
      </Modal>
    </div>
  );
}
