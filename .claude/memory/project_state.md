---
name: project_state
description: The resume point for this repo - current checkpoint (sha, environment, open units table, recommended next unit) at the top, earlier checkpoints below. Read first in every session; rewritten by /recap.
metadata:
  type: project
---

## 2026-08-31 state (resume here)

- **Repo:** `main` @ `1374e8a` - Merge pull request #1 from Kilat-Pet-Delivery/add-mit-license
- **Environment:** dev-infra stack up (`./dev.ps1 up kilat`). Database `kilat_payment` is migrated and clean.
- **Open units**

| Unit / ticket | State | Blocked on | Note |
|---|---|---|---|
| KPD-4 cmd/migrate and migrations applied | In Review | review | PR #3 |
| KPD-60 promos, promo_usages and subscriptions SQL migration | In Review | review | PR #4, stacked on #3. Unblocks KPD-39 |
| KPD-63 gofmt / KPD-6 .env.example | In Review | review | PRs #5, #6 |

- **Recommended next unit:** merge PR #3 then #4, which unblocks KPD-39 (promo apply during checkout).
- **Waiting on Luqman:** merge the open PRs above. Several are stacked, so order matters.

## Earlier checkpoints

(none - this layer was created 2026-08-31 under KPD-51)
