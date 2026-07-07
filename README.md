# Athlete Log

Private fitness analytics you can run on your own machine.

Own your training history. No subscription required to run the core app. No cloud lock-in. No data mining.

[![CI](https://github.com/<your-org>/athlete-log/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/<your-org>/athlete-log/actions/workflows/ci.yml)
[![Docker](https://img.shields.io/badge/Docker-Hub-2496ED?style=flat-square&logo=docker&logoColor=white)](https://hub.docker.com/r/jakenowak99/athlete-log)
[![License: AGPLv3%2B](https://img.shields.io/badge/License-AGPLv3%2B-6366f1?style=flat-square)](LICENSE)

[Docs](docs/setup.md) · [Sources](docs/sources.md) · [Security](docs/security.md) · [Docker Hub](https://hub.docker.com/r/jakenowak99/athlete-log)

| | |
|:---:|:---:|
| ![Activities](docs/assets/screenshots/activities.png) | ![Fitness](docs/assets/screenshots/fitness.png) |
| ![Activity detail](docs/assets/screenshots/activity-detail.png) | ![Heatmap](docs/assets/screenshots/heatmap.png) |

## Quick start

Requires [Docker](https://docs.docker.com/get-docker/).

```sh
git clone https://github.com/<your-org>/athlete-log
cd athlete-log
docker compose up
```

Open `http://127.0.0.1:8080`.

The app starts with a local SQLite database by default. PostgreSQL remains optional for advanced self-hosting.

## What it includes

- Performance Management Chart: CTL, ATL, and TSB
- GPS heatmap
- Activity detail with map and charts
- Personal records
- Training calendar
- Custom dashboards
- Year in Review
- Built-in MCP server for AI tools

## Data sources

- FIT / GPX / TCX file upload
- Watched local folder
- Nike Run Club token import
- Strava OAuth sync when API access is available

## Documentation

- [Setup](docs/setup.md)
- [Connecting sources](docs/sources.md)
- [Features](docs/features.md)
- [Security](docs/security.md)

## Release model

This repository is intended to be the canonical open source codebase for Athlete Log:

- GitHub Releases for tagged versions
- Docker image publishing
- technical docs for install, configuration, sources, and security
- the source of truth for the self-hosted app

## License and trademark

Licensed under [AGPL-3.0-or-later](LICENSE).

See [TRADEMARKS.md](TRADEMARKS.md) for branding and naming rules around forks, hosted services, and commercial distributions.

## Repository notes

- [Rename Plan](RENAME_PLAN.md)
- [Public Repo Layout](PUBLIC_REPO_LAYOUT.md)
- [Private Repo Plan](../private-repo/README.md)
- [Video Backlog](../private-repo/VIDEO_BACKLOG.md)
