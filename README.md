<div align="center">

<img src="docs/assets/logo.svg" alt="Athlete Log" width="80" height="80"/>

## Athlete Log

**Your fitness data, stored on your machine.**  
No subscription. No cloud lock-in. No data mining.

[![CI](https://github.com/nowakkuba99/athlete-log/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/nowakkuba99/athlete-log/actions/workflows/ci.yml)
[![Docker](https://img.shields.io/badge/Docker-Hub-2496ED?style=flat-square&logo=docker&logoColor=white)](https://hub.docker.com/r/jakenowak99/athlete-log)
[![License: AGPLv3%2B](https://img.shields.io/badge/License-AGPLv3%2B-6366f1?style=flat-square)](LICENSE)

[Setup](docs/setup.md) · [Sources](docs/sources.md) · [Features](docs/features.md) · [Security](docs/security.md) · [Docker Hub](https://hub.docker.com/r/jakenowak99/athlete-log)

</div>

---

| | |
|:---:|:---:|
| ![Activities](docs/assets/screenshots/activities.png) | ![Fitness PMC](docs/assets/screenshots/fitness.png) |
| ![Activity detail](docs/assets/screenshots/activity-detail.png) | ![Heatmap](docs/assets/screenshots/heatmap.png) |

---

## Quick start

Requires [Docker](https://docs.docker.com/get-docker/).

```sh
git clone https://github.com/nowakkuba99/athlete-log
cd athlete-log
docker compose up
```

Open **http://127.0.0.1:8080**. The app starts with a local SQLite database by default; Postgres is optional for advanced self-hosting.

See [Setup](docs/setup.md) for pulling the pre-built image and running from a standalone `docker-compose.yml`.

---

## What's inside

- **Performance Management Chart** — CTL, ATL, and TSB via Banister TRIMP; the fitness/fatigue model serious athletes rely on
- **GPS heatmap** — every route overlaid on one map
- **Activity detail** — GPS map, pace/HR/elevation/cadence charts, auto splits, weather chip, comparable efforts, 3D flyover, share card
- **Personal records** — best efforts from 400 m to marathon with progression charts
- **Training calendar** — monthly grid coloured by sport; streak overlay
- **Custom dashboards** — drag-and-resize widgets; set any dashboard as your home page
- **Year in Review** — auto-generated annual summary
- **Built-in MCP server** — connect Claude or other AI tools to your local training data

---

## Getting your data in

| Source | How |
|---|---|
| FIT / GPX / TCX | Upload page — drag and drop, any GPS device |
| Watched folder | Sources → watch a local path (picks up Garmin Express files automatically) |
| Nike Run Club | Sources → paste your NRC bearer token |
| Strava | Requires Strava API access. Settings → Client ID & Secret → Sources → Connect |

No Strava API access? Use Strava's [bulk GDPR export](https://support.strava.com/hc/en-us/articles/216918437) — drag the FIT/GPX files onto the Upload page and your full history is imported.

---

## Stack

Go 1.23 · SQLite by default · PostgreSQL optional · React 18 · Tailwind CSS · Recharts · MapLibre GL · Docker Compose

---

## Documentation

- [Setup](docs/setup.md) — install, configuration, deployment
- [Connecting sources](docs/sources.md) — files, watched folders, Nike Run Club, Strava
- [Features](docs/features.md) — full feature reference
- [Security](docs/security.md) — self-hosting and data-handling guidance

---

## License and trademark

[AGPLv3 or later](LICENSE) — built for athletes who believe their data belongs to them.

See [TRADEMARKS.md](TRADEMARKS.md) for branding and naming rules around forks, hosted services, and commercial distributions.
