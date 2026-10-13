# Connecting Sources

Athlete Log supports four ways to get your training data in. You can use any combination.

---

## Strava

> **Strava API requires a paid subscription.** As of June 2026, Strava [charges $12/month for API access](https://press.strava.com/articles/updates-to-stravas-api-agreement). If you don't have a subscription, use the [File Upload](#file-upload-fit--gpx--tcx) or [Watched Folder](#watched-folder) sources instead — they work with any GPS device and don't require Strava at all.
>
> **Migrating existing Strava data:** go to Strava → Settings → My Account → Download or Delete Your Data. The bulk export contains all your activities as FIT/GPX files. Drop them on the Upload page and they'll be imported with full GPS, HR, and metadata.

Once connected, Strava syncs are incremental — only new activities are fetched after the first full import. A live counter ("N imported") appears on the source card while the sync is running.

### 1. Create a Strava API app

1. Go to [strava.com/settings/api](https://www.strava.com/settings/api)
2. Create an application with any name (e.g. "Athlete Log")
3. Set **Authorization Callback Domain** to `localhost`
4. Copy the **Client ID** and **Client Secret**

### 2. Enter credentials

Either:

- Open **Settings** in the app → paste Client ID and Client Secret → Save, or
- Add to `.env` in the project root:

```
STRAVA_CLIENT_ID=12345
STRAVA_CLIENT_SECRET=abc123...
```

### 3. Connect and sync

1. Open the **Sources** page
2. Click **Connect Strava** — Strava opens in your browser for authorisation
3. After authorising, you land back on Sources with the connection active
4. Click **Sync now**

The first sync imports your full activity history (a few minutes for large accounts). Subsequent syncs are fast.

---

## Nike Run Club

NRC requires a bearer token extracted from your browser.

### Get your bearer token

1. Open [nikeplus.nike.com](https://nikeplus.nike.com) and log in
2. Open Developer Tools → Network tab
3. Reload the page; find any `api.nike.com` request
4. In the request headers, copy the value after `Authorization: Bearer `

### Connect

1. Open the **Sources** page
2. Paste your token into the NRC field → Connect
3. Click **Sync now**

NRC tokens expire. If sync fails with an authentication error, repeat the steps above to get a fresh token.

A live "N imported" counter appears on the source card while the sync is running — NRC provides no total count, so this is a running tally rather than a percentage.

---

## File Upload (FIT / GPX / TCX)

Upload individual workout files from any device.

### Supported formats

| Format | Notes |
|---|---|
| `.fit` | Garmin / Wahoo native format; richest data (HR, cadence, GPS, power) |
| `.gpx` | Universal GPS format; HR and cadence supported via standard extensions |
| `.tcx` | Garmin Training Center XML; laps, HR, cadence |

### How to upload

1. Open the **Upload** page
2. Drag files onto the drop zone, or click to browse
3. Multiple files upload in sequence
4. Each file shows its result: ✓ imported, duplicate (already exists), or error

Uploading the same file twice is safe — deduplication is by SHA-256 of file content.

### Getting files from your device

- **Garmin**: Garmin Express → copies `.fit` files to your computer
- **Apple Watch**: [HealthExport](https://www.healthexportapp.com/) exports workouts as GPX
- **Wahoo**: Wahoo app → activity → share → save `.fit` file
- **Strava**: Settings → My Account → Download or Delete Your Data (bulk GPX)

---

## Watched Folder

The watched folder source monitors a local directory and imports any new `.fit`, `.gpx`, or `.tcx` file that appears there — automatically, without any manual upload step.

This is the most seamless way to import from a Garmin watch: plug in the USB cable, let Garmin Express sync, and Athlete Log picks up the new files in seconds.

### Set up a watched folder

1. Open the **Sources** page
2. Scroll to **Add a source → Watch a local folder**
3. Enter the full path to the directory (e.g. `/Users/you/Downloads/Garmin`)
4. Click **Watch folder**

The folder is scanned immediately on connection. After that, new files are detected via filesystem events (with a 5-minute polling fallback for network drives).

### File log

Each watched-folder source card has an expandable **file log** showing the last 20 processed files with their status:

| Status | Meaning |
|---|---|
| `imported` | New activity created |
| `duplicate` | File already exists (same SHA-256); skipped |
| `error` | Parse or save error; message shown |

### Manual scan

Click **Scan now** on the source card to trigger an immediate full scan of the folder. Useful after copying a batch of files.

### Removing a watched folder

Click the trash icon on the source card. This stops the watcher and removes the source connection. Previously imported activities are not affected.

### Tips

- You can add **multiple folders** — one per device, for example.
- The folder must exist on the machine where Athlete Log is running. For Docker deployments, mount the folder into the container via `docker-compose.yml` (see [Setup](setup.md)).
- NFS / SMB network mounts work; polling ensures files are caught even when filesystem events don't propagate.

---

## Cross-source deduplication

If you sync the **same workout from more than one source** — for example a run recorded simultaneously in Nike Run Club and on your Garmin (which you later upload) — Athlete Log creates only **one** activity. The source that syncs first wins; any later source that finds an existing activity within ±60 seconds of the same sport type silently skips it.

This means you can safely connect all your sources without worrying about duplicate activities cluttering your history.
