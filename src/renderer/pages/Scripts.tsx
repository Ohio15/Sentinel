import { Device, useDeviceStore } from '../stores/deviceStore';
import { useClientStore } from '../stores/clientStore';
import React, { useState, useEffect } from 'react';
import { scripts as scriptsService } from '../services';

interface Script {
  id: string;
  name: string;
  description?: string;
  language: 'powershell' | 'bash' | 'python';
  content: string;
  osTypes: string[];
  createdAt: string;
  updatedAt: string;
}

export function Scripts() {
  const [scripts, setScripts] = useState<Script[]>([]);
  const [loading, setLoading] = useState(true);
  const [showModal, setShowModal] = useState(false);
  const [editingScript, setEditingScript] = useState<Script | null>(null);
  const [selectedScript, setSelectedScript] = useState<Script | null>(null);
  const [osFilter, setOsFilter] = useState<string>('all');
  const [searchTerm, setSearchTerm] = useState('');

  useEffect(() => {
    loadScripts();
  }, []);

  // Filter scripts by OS and search term
  const filteredScripts = scripts.filter(script => {
    const matchesOS = osFilter === 'all' ||
      script.osTypes.length === 0 ||
      script.osTypes.some(os => os.toLowerCase() === osFilter.toLowerCase());
    const matchesSearch = !searchTerm ||
      script.name.toLowerCase().includes(searchTerm.toLowerCase()) ||
      script.description?.toLowerCase().includes(searchTerm.toLowerCase());
    return matchesOS && matchesSearch;
  });

  const loadScripts = async () => {
    setLoading(true);
    try {
      const data = await scriptsService.list();
      setScripts(data as Script[]);
    } catch (error) {
      console.error('Failed to load scripts:', error);
    } finally {
      setLoading(false);
    }
  };

  const handleCreate = () => {
    setEditingScript(null);
    setShowModal(true);
  };

  const handleEdit = (script: Script) => {
    setEditingScript(script);
    setShowModal(true);
  };

  const handleDelete = async (id: string) => {
    if (confirm('Are you sure you want to delete this script?')) {
      await scriptsService.delete(id);
      setScripts(scripts.filter(s => s.id !== id));
      if (selectedScript?.id === id) {
        setSelectedScript(null);
      }
    }
  };

  const handleSave = async (script: Partial<Script>) => {
    if (editingScript) {
      const updated = await scriptsService.update(editingScript.id, script);
      setScripts(scripts.map(s => s.id === editingScript.id ? (updated as Script) : s));
    } else {
      const created = await scriptsService.create(script as { name: string; language: string; content: string; osTypes: string[] });
      setScripts([...scripts, created as Script]);
    }
    setShowModal(false);
  };

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold text-text-primary">Scripts</h1>
        <button onClick={handleCreate} className="btn btn-primary">
          + New Script
        </button>
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-3 gap-6">
        {/* Script List */}
        <div className="lg:col-span-1">
          <div className="card">
            <div className="p-4 border-b border-border space-y-3">
              <h2 className="font-semibold text-text-primary">Script Library</h2>
              {/* Search */}
              <input
                type="text"
                placeholder="Search scripts..."
                value={searchTerm}
                onChange={e => setSearchTerm(e.target.value)}
                className="input text-sm w-full"
              />
              {/* OS Filter */}
              <div className="flex gap-1">
                {['all', 'windows', 'linux', 'macos'].map(os => (
                  <button
                    key={os}
                    onClick={() => setOsFilter(os)}
                    className={`px-3 py-1 text-xs rounded-full transition-colors ${
                      osFilter === os
                        ? 'bg-primary text-white'
                        : 'bg-gray-100 dark:bg-slate-700 text-text-secondary hover:bg-gray-200 dark:hover:bg-slate-600'
                    }`}
                  >
                    {os === 'all' ? 'All' : os === 'macos' ? 'macOS' : os.charAt(0).toUpperCase() + os.slice(1)}
                  </button>
                ))}
              </div>
            </div>
            <div className="divide-y divide-border max-h-[calc(100vh-380px)] overflow-auto">
              {loading ? (
                <p className="p-4 text-text-secondary">Loading scripts...</p>
              ) : filteredScripts.length === 0 ? (
                <p className="p-4 text-text-secondary">
                  {scripts.length === 0 ? 'No scripts yet. Create your first script!' : 'No scripts match the filter.'}
                </p>
              ) : (
                filteredScripts.map(script => (
                  <button
                    key={script.id}
                    onClick={() => setSelectedScript(script)}
                    className={`w-full text-left p-4 hover:bg-gray-50 dark:hover:bg-slate-700 transition-colors ${
                      selectedScript?.id === script.id ? 'bg-primary-light dark:bg-primary/20' : ''
                    }`}
                  >
                    <div className="flex items-start justify-between gap-2">
                      <div className="flex-1 min-w-0">
                        <p className="font-medium text-text-primary truncate">{script.name}</p>
                        <p className="text-sm text-text-secondary truncate">
                          {script.description || 'No description'}
                        </p>
                        <div className="flex gap-1 mt-1.5">
                          {script.osTypes.length > 0 ? (
                            script.osTypes.map(os => (
                              <OsBadge key={os} os={os} />
                            ))
                          ) : (
                            <span className="text-xs text-text-secondary">All OS</span>
                          )}
                        </div>
                      </div>
                      <LanguageBadge language={script.language} />
                    </div>
                  </button>
                ))
              )}
            </div>
          </div>
        </div>

        {/* Script Detail */}
        <div className="lg:col-span-2">
          {selectedScript ? (
            <div className="card">
              <div className="p-4 border-b border-border flex items-center justify-between">
                <div>
                  <h2 className="font-semibold text-text-primary">{selectedScript.name}</h2>
                  <p className="text-sm text-text-secondary">{selectedScript.description}</p>
                </div>
                <div className="flex gap-2">
                  <button onClick={() => handleEdit(selectedScript)} className="btn btn-secondary">
                    Edit
                  </button>
                  <button onClick={() => handleDelete(selectedScript.id)} className="btn btn-danger">
                    Delete
                  </button>
                </div>
              </div>
              <div className="p-4">
                <div className="flex flex-wrap items-center gap-4 mb-4 text-sm text-text-secondary">
                  <div className="flex items-center gap-1.5">
                    <span>Language:</span>
                    <LanguageBadge language={selectedScript.language} />
                  </div>
                  <div className="flex items-center gap-1.5">
                    <span>Target OS:</span>
                    {selectedScript.osTypes.length > 0 ? (
                      <div className="flex gap-1">
                        {selectedScript.osTypes.map(os => (
                          <OsBadge key={os} os={os} />
                        ))}
                      </div>
                    ) : (
                      <span className="text-xs bg-gray-100 dark:bg-gray-700 px-1.5 py-0.5 rounded">All</span>
                    )}
                  </div>
                  <span>Updated: {new Date(selectedScript.updatedAt).toLocaleDateString()}</span>
                </div>
                <pre className="bg-gray-900 text-gray-100 p-4 rounded-lg text-sm overflow-auto max-h-[calc(100vh-500px)] min-h-[200px] font-mono">
                  {selectedScript.content}
                </pre>
              </div>
              <div className="p-4 border-t border-border">
                <ExecuteScriptForm scriptId={selectedScript.id} />
              </div>
            </div>
          ) : (
            <div className="card p-8 text-center">
              <p className="text-text-secondary">Select a script to view details</p>
            </div>
          )}
        </div>
      </div>

      {/* Script Modal */}
      {showModal && (
        <ScriptModal
          script={editingScript}
          onClose={() => setShowModal(false)}
          onSave={handleSave}
        />
      )}
    </div>
  );
}

function LanguageBadge({ language }: { language: string }) {
  const colors: Record<string, string> = {
    powershell: 'bg-blue-100 text-blue-600 dark:bg-blue-900/30 dark:text-blue-400',
    bash: 'bg-green-100 text-green-600 dark:bg-green-900/30 dark:text-green-400',
    python: 'bg-yellow-100 text-yellow-600 dark:bg-yellow-900/30 dark:text-yellow-400',
  };

  return (
    <span className={`badge text-xs ${colors[language] || 'bg-gray-100 text-gray-600 dark:bg-gray-700 dark:text-gray-300'}`}>
      {language}
    </span>
  );
}

function OsBadge({ os }: { os: string }) {
  const osLower = os.toLowerCase();
  const colors: Record<string, string> = {
    windows: 'bg-sky-100 text-sky-700 dark:bg-sky-900/30 dark:text-sky-400',
    linux: 'bg-orange-100 text-orange-700 dark:bg-orange-900/30 dark:text-orange-400',
    macos: 'bg-gray-100 text-gray-700 dark:bg-gray-700 dark:text-gray-300',
  };

  const icons: Record<string, string> = {
    windows: '\uD83E\uDE9F', // Windows logo approximation
    linux: '\uD83D\uDC27',   // Penguin
    macos: '\uD83C\uDF4E',   // Apple
  };

  const displayName = osLower === 'macos' ? 'macOS' : os.charAt(0).toUpperCase() + os.slice(1).toLowerCase();

  return (
    <span className={`inline-flex items-center gap-0.5 px-1.5 py-0.5 rounded text-xs ${colors[osLower] || 'bg-gray-100 text-gray-600 dark:bg-gray-700 dark:text-gray-300'}`}>
      {displayName}
    </span>
  );
}

function ExecuteScriptForm({ scriptId }: { scriptId: string }) {
  const { devices: allDevices } = useDeviceStore();
  const { currentClientId } = useClientStore();
  const [selectedDevices, setSelectedDevices] = useState<string[]>([]);
  const [executing, setExecuting] = useState(false);

  // Derive online devices from device store - single source of truth
  // Also filter by current client if one is selected
  const devices = allDevices.filter(d =>
    d.status === 'online' &&
    (!currentClientId || d.clientId === currentClientId)
  );

  const handleExecute = async () => {
    if (selectedDevices.length === 0) {
      alert('Please select at least one device');
      return;
    }

    setExecuting(true);
    try {
      await scriptsService.run(scriptId, selectedDevices);
      alert('Script execution started');
    } catch (error: unknown) {
      alert(`Error: ${error instanceof Error ? error.message : 'Unknown error'}`);
    } finally {
      setExecuting(false);
    }
  };

  return (
    <div className="space-y-4">
      <h3 className="font-semibold text-text-primary">Execute Script</h3>
      <div>
        <label className="label">Select Devices</label>
        <div className="border border-border rounded-lg max-h-[min(200px,30vh)] overflow-auto">
          {devices.length === 0 ? (
            <p className="p-4 text-text-secondary text-sm">No online devices available</p>
          ) : (
            devices.map(device => (
              <label
                key={device.id}
                className="flex items-center gap-2 p-2 hover:bg-gray-50 cursor-pointer"
              >
                <input
                  type="checkbox"
                  checked={selectedDevices.includes(device.id)}
                  onChange={e => {
                    if (e.target.checked) {
                      setSelectedDevices([...selectedDevices, device.id]);
                    } else {
                      setSelectedDevices(selectedDevices.filter(id => id !== device.id));
                    }
                  }}
                  className="w-4 h-4"
                />
                <span className="text-sm">{device.displayName || device.hostname}</span>
                <span className="text-xs text-text-secondary">{device.osType}</span>
              </label>
            ))
          )}
        </div>
      </div>
      <div className="flex gap-2">
        <button
          onClick={() => setSelectedDevices(devices.map(d => d.id))}
          className="btn btn-secondary text-sm"
        >
          Select All
        </button>
        <button
          onClick={() => setSelectedDevices([])}
          className="btn btn-secondary text-sm"
        >
          Clear
        </button>
        <button
          onClick={handleExecute}
          disabled={executing || selectedDevices.length === 0}
          className="btn btn-primary text-sm ml-auto"
        >
          {executing ? 'Executing...' : `Execute on ${selectedDevices.length} device(s)`}
        </button>
      </div>
    </div>
  );
}

function ScriptModal({
  script,
  onClose,
  onSave,
}: {
  script: Script | null;
  onClose: () => void;
  onSave: (script: Partial<Script>) => void;
}) {
  const [formData, setFormData] = useState({
    name: script?.name || '',
    description: script?.description || '',
    language: script?.language || 'powershell',
    content: script?.content || '',
    osTypes: script?.osTypes || [],
  });

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    onSave(formData);
  };

  const toggleOsType = (os: string) => {
    if (formData.osTypes.includes(os)) {
      setFormData({ ...formData, osTypes: formData.osTypes.filter(t => t !== os) });
    } else {
      setFormData({ ...formData, osTypes: [...formData.osTypes, os] });
    }
  };

  return (
    <div className="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50">
      <div className="bg-surface rounded-lg shadow-xl w-full max-w-2xl p-6 max-h-[90vh] overflow-auto">
        <h2 className="text-lg font-semibold text-text-primary mb-4">
          {script ? 'Edit Script' : 'Create Script'}
        </h2>
        <form onSubmit={handleSubmit} className="space-y-4">
          <div className="grid grid-cols-2 gap-4">
            <div>
              <label className="label">Name</label>
              <input
                type="text"
                value={formData.name}
                onChange={e => setFormData({ ...formData, name: e.target.value })}
                className="input"
                required
              />
            </div>
            <div>
              <label className="label">Language</label>
              <select
                value={formData.language}
                onChange={e => setFormData({ ...formData, language: e.target.value as any })}
                className="input"
              >
                <option value="powershell">PowerShell</option>
                <option value="bash">Bash</option>
                <option value="python">Python</option>
              </select>
            </div>
          </div>
          <div>
            <label className="label">Description</label>
            <input
              type="text"
              value={formData.description}
              onChange={e => setFormData({ ...formData, description: e.target.value })}
              className="input"
            />
          </div>
          <div>
            <label className="label">Target OS (leave empty for all)</label>
            <div className="flex gap-4">
              {['Windows', 'Linux', 'macOS'].map(os => (
                <label key={os} className="flex items-center gap-2">
                  <input
                    type="checkbox"
                    checked={formData.osTypes.includes(os)}
                    onChange={() => toggleOsType(os)}
                    className="w-4 h-4"
                  />
                  <span className="text-sm">{os}</span>
                </label>
              ))}
            </div>
          </div>
          <div>
            <label className="label">Script Content</label>
            <textarea
              value={formData.content}
              onChange={e => setFormData({ ...formData, content: e.target.value })}
              className="input font-mono text-sm"
              rows={15}
              required
            />
          </div>
          <div className="flex justify-end gap-2 pt-4">
            <button type="button" onClick={onClose} className="btn btn-secondary">
              Cancel
            </button>
            <button type="submit" className="btn btn-primary">
              {script ? 'Update' : 'Create'}
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}

export default Scripts;
