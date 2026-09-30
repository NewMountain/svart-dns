import '../../styles/pages/admin.css';
import type { AdminController } from './useAdminController';
export function UsersCard({ model }: { model: AdminController }) {
  const {
    action,
    users,
    currentUsername,
    showCreateUser,
    setShowCreateUser,
    newUsername,
    setNewUsername,
    newPassword,
    setNewPassword,
    newUserRole,
    setNewUserRole,
    editingUserId,
    setEditingUserId,
    editRole,
    setEditRole,
    editPassword,
    setEditPassword,
    confirmDeleteUser,
    setConfirmDeleteUser,
    createUser,
    updateUser,
    deleteUser,
    formatDate,
  } = model;
  return (
    <div className="admin-card">
      <div className="admin-card-header">
        <div>
          <div className="admin-card-title">User Management</div>
          <div className="card-desc">Admin accounts for the web interface.</div>
        </div>
        <button
          className="btn btn-primary"
          onClick={() => {
            setShowCreateUser(!showCreateUser);
          }}
        >
          <svg
            width="16"
            height="16"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            strokeWidth="3"
          >
            <line x1="12" y1="5" x2="12" y2="19" />
            <line x1="5" y1="12" x2="19" y2="12" />
          </svg>
          New User
        </button>
      </div>

      {showCreateUser && (
        <div className="create-panel">
          <div className="admin-form-row">
            <div className="admin-input-group" style={{ flex: 2 }}>
              <div className="admin-label">Username</div>
              <input
                type="text"
                className="admin-input-field"
                placeholder="e.g. sam"
                value={newUsername}
                onChange={(e) => {
                  setNewUsername(e.target.value);
                }}
              />
            </div>
            <div className="admin-input-group" style={{ flex: 2 }}>
              <div className="admin-label">Password</div>
              <input
                type="password"
                className="admin-input-field"
                placeholder="Enter password"
                value={newPassword}
                onChange={(e) => {
                  setNewPassword(e.target.value);
                }}
              />
            </div>
            <div className="admin-input-group" style={{ flex: 1 }}>
              <div className="admin-label">Role</div>
              <select
                className="admin-select"
                value={newUserRole}
                onChange={(e) => {
                  setNewUserRole(e.target.value);
                }}
              >
                <option value="admin">Admin</option>
                <option value="readonly">Read Only</option>
              </select>
            </div>
            <button className="btn btn-primary" onClick={action(createUser)}>
              Create
            </button>
            <button
              className="btn btn-ghost"
              onClick={() => {
                setShowCreateUser(false);
              }}
            >
              Cancel
            </button>
          </div>
        </div>
      )}

      <table className="data-table">
        <thead>
          <tr>
            <th>Username</th>
            <th>Role</th>
            <th>Created</th>
            <th style={{ textAlign: 'right' }}>Actions</th>
          </tr>
        </thead>
        <tbody>
          {users && users.length > 0 ? (
            users.map((u) => {
              const isSelf = u.username === currentUsername;
              const isEditing = editingUserId === u.id;

              if (isEditing) {
                return (
                  <tr key={u.id}>
                    <td style={{ fontWeight: 500 }}>
                      {u.username}
                      {isSelf && <span className="self-badge">(you)</span>}
                    </td>
                    <td>
                      <select
                        className="admin-select admin-select-inline"
                        value={editRole || u.role}
                        onChange={(e) => {
                          setEditRole(e.target.value);
                        }}
                      >
                        <option value="admin">Admin</option>
                        <option value="readonly">Read Only</option>
                      </select>
                    </td>
                    <td>
                      <input
                        type="password"
                        className="admin-input-field admin-input-inline"
                        placeholder="New password (optional)"
                        value={editPassword}
                        onChange={(e) => {
                          setEditPassword(e.target.value);
                        }}
                      />
                    </td>
                    <td
                      style={{
                        textAlign: 'right',
                        display: 'flex',
                        gap: 4,
                        justifyContent: 'flex-end',
                      }}
                    >
                      <button
                        className="btn btn-primary btn-sm"
                        onClick={action(() => updateUser(u.id))}
                      >
                        Save
                      </button>
                      <button
                        className="btn btn-ghost btn-sm"
                        onClick={() => {
                          setEditingUserId(null);
                          setEditRole('');
                          setEditPassword('');
                        }}
                      >
                        Cancel
                      </button>
                    </td>
                  </tr>
                );
              }

              return (
                <tr key={u.id}>
                  <td style={{ fontWeight: 500 }}>
                    {u.username}
                    {isSelf && <span className="self-badge">(you)</span>}
                  </td>
                  <td>
                    <span
                      className={`role-badge ${u.role === 'admin' ? 'role-admin' : 'role-view'}`}
                    >
                      {u.role === 'admin' ? 'Admin' : 'Read Only'}
                    </span>
                  </td>
                  <td>{formatDate(u.created_at)}</td>
                  <td style={{ textAlign: 'right' }}>
                    {!isSelf &&
                      (confirmDeleteUser === u.id ? (
                        <span className="confirm-inline">
                          <span className="text-sm" style={{ color: 'var(--red)' }}>
                            Delete?
                          </span>
                          <button
                            className="btn btn-danger btn-sm"
                            onClick={action(() => deleteUser(u.id))}
                          >
                            Yes
                          </button>
                          <button
                            className="btn btn-ghost btn-sm"
                            onClick={() => {
                              setConfirmDeleteUser(null);
                            }}
                          >
                            No
                          </button>
                        </span>
                      ) : (
                        <>
                          <button
                            className="admin-btn-edit"
                            title="Edit"
                            aria-label="Edit user"
                            onClick={() => {
                              setEditingUserId(u.id);
                              setEditRole(u.role);
                              setEditPassword('');
                            }}
                          >
                            <svg
                              width="16"
                              height="16"
                              viewBox="0 0 24 24"
                              fill="none"
                              stroke="currentColor"
                              strokeWidth="2"
                            >
                              <path d="M11 4H4a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-7" />
                              <path d="M18.5 2.5a2.121 2.121 0 0 1 3 3L12 15l-4 1 1-4 9.5-9.5z" />
                            </svg>
                          </button>
                          <button
                            className="admin-btn-icon"
                            title="Delete"
                            aria-label="Delete user"
                            onClick={() => {
                              setConfirmDeleteUser(u.id);
                            }}
                          >
                            <svg
                              width="16"
                              height="16"
                              viewBox="0 0 24 24"
                              fill="none"
                              stroke="currentColor"
                              strokeWidth="2"
                            >
                              <polyline points="3 6 5 6 21 6" />
                              <path d="M19 6l-1 14a2 2 0 0 1-2 2H8a2 2 0 0 1-2-2L5 6" />
                              <path d="M10 11v6" />
                              <path d="M14 11v6" />
                              <path d="M9 6V4a1 1 0 0 1 1-1h4a1 1 0 0 1 1 1v2" />
                            </svg>
                          </button>
                        </>
                      ))}
                  </td>
                </tr>
              );
            })
          ) : (
            <tr>
              <td
                colSpan={4}
                style={{ textAlign: 'center', color: 'var(--text-muted)', padding: 32 }}
              >
                No admin users created yet
              </td>
            </tr>
          )}
        </tbody>
      </table>
    </div>
  );
}
