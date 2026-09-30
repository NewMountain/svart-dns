# Frontend Design

This document is intended to create a visual language and design for
the Svart DNS application. For additional details about the vision of
Svart DNS, see VISION.md.

The goal here is produce a visual design that is crisp, information rich,
actionable, intuitive and beautiful. Most DNS solutions are a drab enterprise
technology, the goal here is to produce something that is legitimately
exciting, visually appealing and delightful. Having that said, it's a highly
functional tool, not just aesthetic for aesthetic's sake.

For some napkin designs, see overall_design image.

## Assets required

Please produce a simple but visually distinct icon for the application top left "home" and
Favicon as well as PWA icon for mobile. The icon should honor the inspiration for this project:
the song "Your Black is my White" a Swedish metal song from Feard's album "Svart".

Much like the song, the application should be ruthlessly efficient and brutal allowing you
instant insight into all users, their patterns, use (and misuse) of devices leaking information
and secrets and general privacy leaks with the goal of keeping everyone's privacy maximized
by ruthlessly eliminating traffic from bots/scripts/non-concensual telemetry, etc.

Additionally, early UI mockups in image format should be used to idea every screen and,
once agreed on, functioning HTML/CSS/JS should be provided for integration back into the existing
backend codebase. For the purpose of demo, test and mock JSON data can be created.

Please refer to SCHEMA.md for additional guidance on backend resources.

If additional APIs or schemas as required, you are able to state that as part of this.

## Visual Language

A growing body of internal applicaitons use a slightly modified (darker background)
pallette based on Monokai Pro's Spectrum colorset. The user fell in love with computers via
Sublime text, so this pallette feels like "home" for them.

Backgrounds: #09090b (darkest), #18181b (page), #1f1f22 (card/surface) Borders: #27272a (default), #3f3f46 (hover) Muted text: #71717a Accents: #ff6188 (red), #fc9867 (orange), #ffd866 (yellow), #a9dc76 (green), #78dce8 (blue), #ab9df2 (purple) Fonts: Inter (UI), JetBrains Mono (data/code)

## Sections

The core sections of the app (selected via the sidebar) are

* Dashboard
* Config
* Rewrites
* Filters
* Tiers
* Logs
* Analysis
* Admin

At the bottom of the sidebar, there should be a mention of the logged in user and a button to log out

### Dashboard

The dashboard is highly inspired by other apps in the space, Technitium, AdGuard and PiHole.
Our dashboard is highly visual, interactive and visually vibrant. We should strive to be as chart
heavy as possible. The charts should be useful and answer questions at a glance, they should not be wonky.
We should show high level metrics such as:

* DNS Queries in various windows (with some average trendline for comparison)
  * Alternate graphic: show as bargraph by client over time
* Queries blocked by filters (with some average trendline for comparison)
  * Alternate graphic: show as bargraph by client over time
* Percentage of queries blocked (with some average trendline for comparison)
  * Alternate graphic: show as bargraph by client over time
* Average Query Response time (with some average trendline for comparison)
* Piechart breakdown of percent of DNS blocked by which list
* Piechart breakdown of overrides (block/allow) by custom rule
* Top clients (with some average trendline for comparison)
* Top Queried Domains over various windows
* Top Blocked Domains over various windows
* Top Upstream Resolvers (by count)
* Average Upstream resolver response (with some average trendline for comparison)

These are some ideas, please propose other interesting metrics based on what you know
is available and in line with the Vision doc.

### Config

This section should be more settings and doesn't need to be chart rich
(it likely won't) have any charts. The idea here is to set logging periods
(default to 2 years), archiving output dir (default to environment variable)
Whether logging is enabled or not and whether client IPs should be anonimized.
User should also be able to state their preferred timezone
That's basically it.

### Rewrites

This section should show a simple list of given a domain (simple or wildcard). Each row should show

* enabled (checkbox)
* Domain
* Resolves to IP
* Ideally, some basic analytics (24 hour hit count, clients hit, etc)
* Edit Button
* Delete Button

### Filters

Filters should be a multi-part section. In this section, we want

* Published Lists
  * This section should allow us to enter in a URL to add a published list
  * When a published list is added, a process will kick of to ingest the published list
  * There should be a way, to modify the refresh period (default 7 days)
  * Once ingested, you should see last refreshed, number of domains, details and history
  * History should show a list of every update, when it happened and modifications (2 domains added, 3 removed, etc)
  * Details should open a page to show the entire list of domains associated with that published list
    * Either the present version, or a version in the past
* History
  * Discussed Above, make this a tabbed section in the multi-part section
* Details
  * Discussed above, make this a tabbed section in the multi-part section

### Tiers

Tiers should be a multi-part section. In this section we want

* Ranges
  * This should allow us to CRUD ranges
  * Ranges are CIDR IP ranges
  * The list view should show the name of the range, the CIDR and "noob friendly" CIDR as well as
    custom rule count (count of blocks (red), count of allows (green), total count (some other color))
  * The default should be a list of ranges with a button to create a range
  * The CRUD should open a Range Details page where you can
    * Name the range
    * Define the CIDR range of the range
    * Have some nice UI to make CIDR ranges more intuitive for users (visualize that 10.42.1.1/24) is actually 10.42.0-255.0-255 sort of thing
    * You should be able to add "custom rules" to a range here. They allow you to block/allow a domain (plain or wildcard)
    * It would be interesting to see a small querylog of recent queries from this range
* Groups
  * This should allow us to CRUD groups
  * Groups are user defined collections of IPS
  * The list view should show the name of the group, count of IPs in group, as well as
    custom rule count (count of blocks (red), count of allows (green), total count (some other color))
  * The default should be a list of groups with a button to create a group
  * The CRUD should open a Group Details page where you can
    * Name the group
    * Add/remove users/IPs to the group from a dropdown of all known users (show IP and, if available user name)
    * You should be able to add "custom rules" to a groups here. They allow you to block/allow a domain (plain or wildcard)
    * It would be interesting to see a small querylog of recent queries from this group
* IPs
  * This should allow us to CRUD users (IPs)
  * The list view should show the name of the group, the IP of the user as well as
    custom rule count (count of blocks (red), count of allows (green), total count (some other color))
  * The default should be a list of all users ever observed
  * The CRUD should open a user Details page where you can
    * Name the user
    * You should be able to add "custom rules" to a groups here. They allow you to block/allow a domain (plain or wildcard)
    * It would be interesting to see a small querylog of recent queries from this user
  
### Logs

The logs should just be a table of logs. The user should have a "search"
option where they can look for logs. There should be a "live" option where you are looking at a live
feed of logs (refreshed every N seconds configurable by some user drop down) or allow the user to select
a datetime range.

The user should have the ability to filter by user, by group, by range, as well as search by a domain
(simple or wildcard). Each result should state:

* its time (in user's preferred timezone)
* the request/query
* the response (blocked, allowed, rewritten)
* the response time
* the client (IP and name)
* any ranges the client is in
* any groups the client is in
* the source of the decision
* the evaluation logic
* a cog
* Selecting the cog will allow you to:
  * block/allow this domain in a tier (group, or range)
  * block/allow this domain for a user

### Analysis

Arguably the most differentiated feature of this entire app. It's the "killer app"

So I want to go to a tab and see, for one or many domains on the left in a row and for all publish lists
(ordered from smallest count to largest count) as columns I just want to see a matrix of
red X/green check as to whether a request would be allowed. Simple. No need to consider user, just basic.
We should also have some way to prefill the left rows with domains (top 10k websites,
top 1k user domains, top N domains in last week etc).

In my head, there's a second more "advanced" page where you can see, for a given user
(you can select to manually type) an IP and then simulate the decision tree for the same list of domains.
In this view, the columns would be the tiers and each one would be expandable to show you
the calculations in that tier.

Filters should be a multi-part section. In this section, we want

1. Domain × List Matrix — domains as rows, published lists as columns (smallest→largest), red X / green check
cells. Prefill from Tranco top 10k, top queried domains, top blocked, etc.
2. Policy Simulator — same domain rows, but you enter a client IP. Columns become the three tiers (Range,
Group, IP), each expandable to show the decision breakdown per tier.

All three backend endpoints (/api/analysis/matrix, /api/analysis/simulate, /api/analysis/domains) were built to
serve exactly this vision.

POST /api/analysis/matrix — takes a list of domains, returns a domain × blocklist membership grid.
Each row is a domain, each column is a blocklist (sorted smallest-first), cells are boolean hits
with the specific matched rule. This is the "which lists block which domains" heatmap.

POST /api/analysis/compare — pairwise overlap between blocklists. Symmetric matrix showing how many
domains two lists share, plus unique-to-list counts.

POST /api/analysis/simulate — batch policy evaluation per client. "What would happen to these domains
for this client given their current tier assignments?"

GET /api/analysis/domains — prefill sources (Tranco top 10k, top queried, top blocked, top allowed from logs).

The vision you described was essentially: pick a set of domains (from real traffic or Tranco), pick your
blocklists, and see a heatmap/matrix showing coverage — answering "blocklist FOMO" by showing empirically what
each list catches that others don't. The compare endpoint gives the overlap story (redundancy between lists),
and simulate shows the policy engine's actual decision for a client.

### Admin

Admin portal should only appear for admins
Admin portal allows you to:

* Create/Modify/Delete Users
  * A user has a name and a role (Admin or viewer)
* Create/Modify/Delete API Keys
  * An API Key has a:
    * name
    * Description
    * the key value itself (show as stars with a button to reveal)
    * The day the key was issued
