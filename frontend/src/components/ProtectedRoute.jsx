// Phase 4: wire this to GET /api/v1/auth/me and redirect to /login on 401.
// For now it passes through unconditionally (localhost-only, no auth in v1).
export default function ProtectedRoute({ children }) {
  return children;
}
