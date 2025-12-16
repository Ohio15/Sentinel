# Multi-Client/Tenant Feature Implementation Plan

## Overview
Add client/organization grouping to Sentinel so users can:
1. Select a client context when opening the app
2. Filter all views (devices, tickets, alerts) to only that client's data
3. View a global dashboard showing alerts/problems across ALL clients

## Database Schema Changes

### 1. Create `clients` table
```sql
CREATE TABLE clients (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL UNIQUE,
  description TEXT,
  color TEXT,           -- For UI identification
  logo_url TEXT,        -- Optional client logo
  created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
```

### 2. Add `client_id` foreign key to existing tables
- `devices` - add `client_id TEXT REFERENCES clients(id)`
- `tickets` - add `client_id TEXT REFERENCES clients(id)`
- `alert_rules` - add `client_id TEXT REFERENCES clients(id)`
- `scripts` - add `client_id TEXT REFERENCES clients(id)` (optional, for client-specific scripts)

### 3. Migration strategy
- Add columns as nullable initially
- Existing data gets NULL client_id (treated as "unassigned")
- Create UI to assign existing devices to clients

## State Management

### 1. Create `clientStore.ts`
```typescript
interface ClientState {
  clients: Client[];
  currentClientId: string | null;  // null = "All Clients" view
  loading: boolean;
  error: string | null;

  fetchClients: () => Promise<void>;
  setCurrentClient: (clientId: string | null) => void;
  createClient: (client: Partial<Client>) => Promise<void>;
  updateClient: (id: string, client: Partial<Client>) => Promise<void>;
  deleteClient: (id: string) => Promise<void>;
}
```

### 2. Modify existing stores to filter by client
- `deviceStore.fetchDevices()` - filter by currentClientId
- `ticketStore.fetchTickets()` - filter by currentClientId
- `alertStore.fetchAlerts()` - filter by currentClientId

## UI Components

### 1. Client Selector (Header/Sidebar)
- Dropdown in the header showing current client
- "All Clients" option for global view
- Quick-switch between clients
- Persists selection to localStorage

### 2. Client Management Page
- List all clients
- Create/Edit/Delete clients
- Assign color and optional logo
- View device count per client

### 3. Device Assignment
- Add "Client" field to device details
- Bulk-assign devices to clients
- When agent first connects, prompt to assign to client

### 4. Global Alerts Dashboard
- New page/widget showing alerts across ALL clients
- Grouped by client for easy identification
- Quick navigation to specific client's alerts
- Badge/indicator showing critical alerts per client

## API/IPC Changes

### 1. New IPC handlers
- `clients:list` - Get all clients
- `clients:get` - Get single client
- `clients:create` - Create client
- `clients:update` - Update client
- `clients:delete` - Delete client
- `devices:assign-client` - Assign device to client
- `devices:bulk-assign-client` - Bulk assign devices

### 2. Modified IPC handlers
- `devices:list` - Add optional `clientId` filter parameter
- `tickets:list` - Add optional `clientId` filter parameter
- `alerts:list` - Add optional `clientId` filter parameter

## Implementation Order

### Phase 1: Database & Backend
1. Create migration for `clients` table
2. Add `client_id` columns to devices, tickets, alert_rules
3. Create Client model and database operations
4. Add IPC handlers for client CRUD

### Phase 2: State Management
5. Create `clientStore.ts`
6. Add client context to existing stores
7. Modify fetch functions to accept clientId filter

### Phase 3: UI - Client Selector
8. Create ClientSelector component
9. Add to app header/navigation
10. Persist selection to localStorage
11. Wire up to clientStore

### Phase 4: UI - Client Management
12. Create ClientsPage for managing clients
13. Add to navigation
14. Create/Edit client forms
15. Device assignment UI

### Phase 5: Global Alerts View
16. Create GlobalAlertsPage or dashboard widget
17. Aggregate alerts across all clients
18. Group by client with visual indicators

### Phase 6: Polish
19. Add client badges to device/ticket lists
20. Migration UI for existing data
21. Agent enrollment client assignment flow
