# RecoverAI Dashboard

Next.js 14 dashboard for RecoverAI revenue recovery platform.

## Features

- **Dark mode only** - Optimized for operations dashboards
- **Real-time updates** - SWR polling every 5 seconds for live data
- **Three main views:**
  - `/dashboard` - Overview with 6 metric cards, revenue charts, live feed
  - `/dashboard/cases` - Filterable recovery cases table
  - `/dashboard/cases/[id]` - Full audit timeline with validator checks, AI decisions, policy rules

## Tech Stack

- Next.js 14 (App Router)
- TypeScript
- Tailwind CSS
- shadcn/ui components
- SWR for data fetching
- Recharts for visualizations
- date-fns for date formatting

## Getting Started

1. Install dependencies:
```bash
npm install
```

2. Create `.env.local`:
```bash
NEXT_PUBLIC_API_URL=http://localhost:8080/api/v1
```

3. Run development server:
```bash
npm run dev
```

4. Open [http://localhost:3000](http://localhost:3000)

## Project Structure

```
src/
├── app/
│   ├── dashboard/
│   │   ├── page.tsx              # Overview page
│   │   ├── cases/
│   │   │   ├── page.tsx          # Cases list
│   │   │   └── [id]/page.tsx     # Case detail
│   │   └── analytics/
│   │       └── page.tsx          # Analytics page
│   ├── layout.tsx                # Root layout with sidebar
│   └── globals.css               # Dark theme CSS variables
├── components/
│   └── ui/                       # shadcn/ui components
│       ├── card.tsx
│       ├── badge.tsx
│       └── button.tsx
└── lib/
    ├── api.ts                    # API client
    ├── types.ts                  # TypeScript types
    └── utils.ts                  # Helper functions

```

## API Integration

The dashboard connects to the Go backend API at `/api/v1`:

- `GET /analytics/overview` - Dashboard metrics
- `GET /analytics/recovery-rate` - Recovery rate by failure type
- `GET /analytics/revenue` - Revenue time series
- `GET /analytics/honest-exceptions` - Failed recovery cases
- `GET /analytics/ai-performance` - AI performance metrics
- `GET /recovery-cases` - List all cases (filterable)
- `GET /recovery-cases/:id` - Case details
- `GET /recovery-cases/:id/audit-logs` - Full audit timeline

## Build for Production

```bash
npm run build
npm start
```

## Docker

The dashboard is included in the main `docker-compose.yml`:

```bash
docker-compose up frontend
```
