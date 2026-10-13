const BASE = "/api/v1";

export function getAuthToken() { return sessionStorage.getItem("av_auth_token") ?? ""; }
export function setAuthToken(t) { sessionStorage.setItem("av_auth_token", t); }
export function clearAuthToken() { sessionStorage.removeItem("av_auth_token"); }

async function request(path, options = {}) {
  const { headers, ...rest } = options;
  const token = getAuthToken();
  const authHeader = token ? { Authorization: `Bearer ${token}` } : {};
  const res = await fetch(BASE + path, {
    headers: { "Content-Type": "application/json", ...authHeader, ...headers },
    ...rest,
  });
  if (res.status === 401) {
    clearAuthToken();
    window.dispatchEvent(new Event("av:auth-required"));
    throw new Error("authentication required");
  }
  if (!res.ok) {
    const body = await res.json().catch(() => ({}));
    throw new Error(body.error || `${res.status} ${res.statusText}`);
  }
  return res.json();
}

export const api = {
  health: () => fetch(BASE + "/health").then((r) => r.json()),
  auth: {
    verify: (password) =>
      fetch(BASE + "/auth/verify", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ password }),
      }).then((r) => ({ ok: r.ok })),
  },

  activities: {
    list: (params = {}) =>
      request("/activities?" + new URLSearchParams(filterEmpty(params))),
    get: (id) => request(`/activities/${id}`),
    update: (id, body) => request(`/activities/${id}`, { method: "PATCH", body: JSON.stringify(body) }),
    delete: (id) => request(`/activities/${id}`, { method: "DELETE" }),
    samples: (id, resolution = 200) =>
      request(`/activities/${id}/samples?resolution=${resolution}`),
    similar: (id) => request(`/activities/${id}/similar`),
    setTag: (id, key, value) =>
      request(`/activities/${id}/tags/${encodeURIComponent(key)}`, {
        method: "PUT",
        body: JSON.stringify({ value }),
      }),
    deleteTag: (id, key) =>
      fetch(`/api/v1/activities/${id}/tags/${encodeURIComponent(key)}`, { method: "DELETE" }),
    weather: (id) => request(`/activities/${id}/weather`),
    cardUrl: (id, theme = "dark", units = "metric") =>
      `${BASE}/activities/${id}/card.svg?theme=${theme}&units=${units}`,
  },

  stats: {
    summary: (params = {}) =>
      request("/stats?" + new URLSearchParams(filterEmpty(params))),
    timeseries: (params = {}) =>
      request("/stats/timeseries?" + new URLSearchParams(filterEmpty(params))),
    streaks: () => request("/stats/streaks"),
    bests: () => request("/stats/bests"),
    pmc: (params = {}) => request("/stats/pmc?" + new URLSearchParams(filterEmpty(params))),
    yearReview: (year) => request(`/stats/year-review?year=${year}`),
  },

  sources: {
    list: () => request("/sources"),
    connectNRC: (token) =>
      request("/sources/nrc/connect", {
        method: "POST",
        body: JSON.stringify({ token }),
      }),
    connectStravaUrl: () => BASE + "/sources/strava/connect",
    connectFolder: (path) =>
      request("/sources/folder/connect", {
        method: "POST",
        body: JSON.stringify({ path }),
      }),
    deleteFolder: (id) =>
      request(`/sources/folder/${id}`, { method: "DELETE" }),
    sync: (id) => request(`/sources/${id}/sync`, { method: "POST" }),
    files: (id) => request(`/sources/${id}/files`),
  },

  jobs: {
    list: (limit = 20) => request(`/jobs?limit=${limit}`),
    get: (id) => request(`/jobs/${id}`),
  },

  uploads: {
    upload: async (file) => {
      const form = new FormData();
      form.append("file", file);
      const token = getAuthToken();
      const res = await fetch(BASE + "/uploads", {
        method: "POST",
        headers: token ? { Authorization: `Bearer ${token}` } : {},
        body: form,
      });
      if (!res.ok) {
        const body = await res.json().catch(() => ({}));
        throw new Error(body.error || `${res.status} ${res.statusText}`);
      }
      return res.json();
    },
  },

  exports: {
    create: (params = {}) =>
      request("/exports", {
        method: "POST",
        body: JSON.stringify(filterEmpty(params)),
      }),
  },

  artifacts: {
    downloadUrl: (id) => BASE + `/artifacts/${id}/download`,
  },

  settings: {
    get: () => request("/settings"),
    save: (body) => request("/settings", { method: "PUT", body: JSON.stringify(body) }),
  },

  profile: {
    get: () => request("/profile"),
    update: (body) => request("/profile", { method: "PUT", body: JSON.stringify(body) }),
    uploadAvatar: async (file) => {
      const form = new FormData();
      form.append("avatar", file);
      const res = await fetch(BASE + "/profile/avatar", { method: "POST", body: form });
      if (!res.ok) {
        const b = await res.json().catch(() => ({}));
        throw new Error(b.error || `${res.status}`);
      }
      return res.json();
    },
    avatarUrl: () => BASE + "/profile/avatar",
  },

  gear: {
    list: (retired = false) => request(`/gear${retired ? "?retired=true" : ""}`),
    create: (body) => request("/gear", { method: "POST", body: JSON.stringify(body) }),
    update: (id, body) => request(`/gear/${id}`, { method: "PUT", body: JSON.stringify(body) }),
    remove: (id) => request(`/gear/${id}`, { method: "DELETE" }).catch(() => null),
    getForActivity: (activityId) => request(`/activities/${activityId}/gear`),
    setForActivity: (activityId, gearIds) =>
      request(`/activities/${activityId}/gear`, { method: "PUT", body: JSON.stringify({ gear_ids: gearIds }) }),
  },

  records: {
    list: (sportType = "") =>
      request("/records" + (sportType ? `?sport_type=${sportType}` : "")),
  },

  dashboards: {
    list: () => request("/dashboards"),
    create: (name) => request("/dashboards", { method: "POST", body: JSON.stringify({ name }) }),
    get: (id) => request(`/dashboards/${id}`),
    update: (id, name) => request(`/dashboards/${id}`, { method: "PUT", body: JSON.stringify({ name }) }),
    remove: (id) => request(`/dashboards/${id}`, { method: "DELETE" }).catch(() => null),
  },

  widgets: {
    list: (dashboardId) => request(`/dashboards/${dashboardId}/widgets`),
    create: (dashboardId, type, config = {}, position = { x: 0, y: 0, w: 4, h: 3 }) =>
      request(`/dashboards/${dashboardId}/widgets`, {
        method: "POST",
        body: JSON.stringify({ type, config, position }),
      }),
    update: (dashboardId, widgetId, patch) =>
      request(`/dashboards/${dashboardId}/widgets/${widgetId}`, {
        method: "PUT",
        body: JSON.stringify(patch),
      }),
    remove: (dashboardId, widgetId) =>
      request(`/dashboards/${dashboardId}/widgets/${widgetId}`, { method: "DELETE" }).catch(() => null),
  },
};

function filterEmpty(obj) {
  return Object.fromEntries(
    Object.entries(obj).filter(([, v]) => v !== "" && v != null)
  );
}
