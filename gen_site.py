#!/usr/bin/env python3
"""Generate static HTML for all briefs and index pages."""
import calendar
import json
import os
import re
import sys
from html import escape
from pathlib import Path

SITE_DIR = Path(__file__).parent / "site"

# ---------------------------------------------------------------------------
# Shared helpers
# ---------------------------------------------------------------------------

HEAD_COMMON = """
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <link rel="preconnect" href="https://fonts.googleapis.com">
  <link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
  <link href="https://fonts.googleapis.com/css2?family=Crimson+Text:wght@400;600;700&family=Instrument+Sans:wght@400;500;600;700&family=JetBrains+Mono:wght@400;500&display=swap" rel="stylesheet">
  <link rel="stylesheet" href="/static/style.css">
  <link rel="alternate" type="application/rss+xml" title="AI Friday Daily Brief" href="https://aifri.day/feed.xml">
  <link rel="icon" href="/favicon.ico" sizes="48x48">
  <link rel="icon" href="/icon-192.png" type="image/png" sizes="192x192">
  <link rel="apple-touch-icon" href="/apple-touch-icon.png">
  <script defer src="https://umami-production-d337.up.railway.app/script.js" data-website-id="3a8001f6-6312-473a-b2bd-38dae609847c"></script>
  <script src="https://analytics.ahrefs.com/analytics.js" data-key="23HVW+pAWhX3mLQMX/nA5A" async></script>
""".strip()

NAV_LOGO = '<a href="/" class="nav-logo"><span class="logo-ai">AI</span><span class="logo-friday">&thinsp;Friday</span></a>'

MONTH_NAMES = ["", "January", "February", "March", "April", "May", "June",
               "July", "August", "September", "October", "November", "December"]

# Feed sources by category — mirrors feeds/feeds.go
SOURCES = {
    "News": [
        {"name": "TechCrunch AI", "url": "https://techcrunch.com/category/artificial-intelligence/"},
        {"name": "The Verge AI", "url": "https://www.theverge.com/ai-artificial-intelligence"},
        {"name": "Ars Technica AI", "url": "https://arstechnica.com/ai/"},
        {"name": "MIT Technology Review AI", "url": "https://www.technologyreview.com/topic/artificial-intelligence/"},
    ],
    "Blogs & Labs": [
        {"name": "Simon Willison", "url": "https://simonwillison.net"},
        {"name": "OpenAI Blog", "url": "https://openai.com/news"},
        {"name": "Google AI Blog", "url": "https://blog.google/technology/ai/"},
        {"name": "Google DeepMind", "url": "https://deepmind.google/discover/blog/"},
        {"name": "Hugging Face Blog", "url": "https://huggingface.co/blog"},
    ],
    "Newsletters": [
        {"name": "Latent Space", "url": "https://latent.space"},
        {"name": "Ben's Bites", "url": "https://bensbites.com"},
        {"name": "Import AI", "url": "https://importai.substack.com"},
    ],
    "Podcasts": [
        {"name": "AI Daily Brief", "url": "https://podcasts.apple.com/us/podcast/the-ai-daily-brief/id1680633614"},
        {"name": "AI & I (Every.to)", "url": "https://every.to/podcast"},
        {"name": "How I AI", "url": "https://www.howiai.com"},
        {"name": "Behind the Craft", "url": "https://behindthecraft.fm"},
        {"name": "AI for Humans", "url": "https://aiforhumans.show"},
        {"name": "a16z AI", "url": "https://a16z.com/podcasts/"},
    ],
    "Community": [
        {"name": "Hacker News", "url": "https://news.ycombinator.com"},
        {"name": "AI Friday Slack", "url": "https://aifri.day"},
    ],
}


def _head(title, description, canonical, og_type="website", extra_meta="", json_ld=None, json_ld_blocks=None):
    if json_ld_blocks:
        ld_block = "".join(f'\n  <script type="application/ld+json">\n  {block}\n  </script>' for block in json_ld_blocks)
    elif json_ld:
        ld_block = f'\n  <script type="application/ld+json">\n  {json_ld}\n  </script>'
    else:
        ld_block = ""
    return f"""  <title>{title}</title>
  <meta name="description" content="{description}">
  <link rel="canonical" href="{canonical}">
  <meta property="og:title" content="{title}">
  <meta property="og:description" content="{description}">
  <meta property="og:url" content="{canonical}">
  <meta property="og:type" content="{og_type}">
  <meta property="og:image" content="https://aifri.day/og-default.png">
  <meta property="og:site_name" content="AI Friday">
  <meta name="twitter:card" content="summary">
  <meta name="twitter:title" content="{title}">
  <meta name="twitter:description" content="{description}">
  <meta name="twitter:image" content="https://aifri.day/og-default.png">{extra_meta}{ld_block}"""


def _breadcrumbs(crumbs):
    """crumbs: list of (label, url) — last item has no link."""
    parts = []
    for i, (label, url) in enumerate(crumbs):
        if i == len(crumbs) - 1:
            parts.append(f'<span class="bc-current">{label}</span>')
        else:
            parts.append(f'<a href="{url}" class="bc-link">{label}</a>')
    items_html = "".join(f'<li>{p}</li>' for p in parts)
    return f'<nav class="breadcrumbs" aria-label="breadcrumb"><ol>{items_html}</ol></nav>'


def _nav(back_href=None, back_label=None):
    back = ""
    if back_href and back_label:
        back = f'<a href="{back_href}" class="nav-back">&larr; {back_label}</a>'
    return f'''<nav class="nav">
      {NAV_LOGO}
      <div class="nav-links">{back}</div>
    </nav>'''


def _sources_section_html():
    """Render the grouped sources section for the /brief/ index."""
    groups = []
    for category, srcs in SOURCES.items():
        links = "".join(
            f'<li><a href="{escape(s["url"])}" rel="noopener">{escape(s["name"])}</a></li>'
            for s in srcs
        )
        groups.append(f'<div class="sources-group"><h3 class="sources-group-label">{escape(category)}</h3><ul class="sources-list">{links}</ul></div>')
    return "<div class=\"sources-groups\">" + "\n".join(groups) + "</div>"


# ---------------------------------------------------------------------------
# Individual brief page
# ---------------------------------------------------------------------------

def render_brief(data):
    """Render a brief JSON into a full HTML page."""
    sections_html = []
    for section in data["sections"]:
        items_html = "\n".join(_render_item(item) for item in section["items"])
        extra_class = " community-section" if _is_community_section(section["title"]) else ""
        sections_html.append(f'''<section class="brief-section{extra_class}">
  <h2>{escape(section["title"])}</h2>
  {items_html}
</section>''')

    quick_links_html = "\n".join(_render_quick_link(ql) for ql in data.get("quick_links", []))
    sources_raw = data.get("sources", [])
    sources_html = []
    for s in sources_raw:
        if isinstance(s, dict) and "url" in s and "name" in s:
            sources_html.append(f'<li><a href="{escape(s["url"])}">{escape(s["name"])}</a></li>')
    sources_html = "\n".join(sources_html)

    prev_date = data.get("prev_date", "")
    next_date = data.get("next_date", "")
    prev_html = f'''<a href="/brief/{prev_date}/" class="brief-nav-link brief-nav-link--prev">
        <span class="brief-nav-label">&larr; Previous</span>
        <span class="brief-nav-date">{prev_date}</span>
      </a>''' if prev_date else '<div class="brief-nav-placeholder"></div>'
    next_html = f'''<a href="/brief/{next_date}/" class="brief-nav-link brief-nav-link--next">
        <span class="brief-nav-label">Next &rarr;</span>
        <span class="brief-nav-date">{next_date}</span>
      </a>''' if next_date else '<div class="brief-nav-placeholder"></div>'

    lede_html = data.get("lede", "")
    date_path = data["date_path"]
    parts = date_path.split("/")
    year, month = parts[0], parts[1]
    month_name = MONTH_NAMES[int(month)]
    escaped_date = escape(data["date"])
    iso_date = date_path.replace('/', '-')

    # SEO
    brief_preview_raw = re.sub(r'<[^>]+>', '', lede_html)[:160]
    if len(brief_preview_raw) >= 160:
        brief_preview_raw = brief_preview_raw[:brief_preview_raw.rfind(' ')] + '\u2026'
    brief_preview = escape(brief_preview_raw)

    breadcrumb_crumbs = [
        ("AI Friday", "https://aifri.day/"),
        ("Daily Brief", "https://aifri.day/brief/"),
        (str(year), f"https://aifri.day/brief/{year}/"),
        (month_name, f"https://aifri.day/brief/{year}/{month}/"),
        (escaped_date, None),
    ]
    bc_json = json.dumps({
        "@context": "https://schema.org",
        "@type": "BreadcrumbList",
        "itemListElement": [
            {"@type": "ListItem", "position": i+1, "name": label,
             "item": url if url else f"https://aifri.day/brief/{date_path}/"}
            for i, (label, url) in enumerate(breadcrumb_crumbs)
        ]
    }, indent=2)
    article_json = json.dumps({
        "@context": "https://schema.org",
        "@type": "Article",
        "headline": f"AI Friday Daily Brief — {data['date']}",
        "datePublished": iso_date,
        "url": f"https://aifri.day/brief/{date_path}/",
        "publisher": {
            "@type": "Organization",
            "name": "AI Friday",
            "url": "https://aifri.day"
        }
    }, indent=2)
    bc_html = _breadcrumbs([
        ("Brief", "/brief/"),
        (year, f"/brief/{year}/"),
        (month_name, f"/brief/{year}/{month}/"),
        (escaped_date, None),
    ])

    return f'''<!DOCTYPE html>
<html lang="en">
<head>
  {HEAD_COMMON}
  {_head(
      f"AI Friday — {data['date']}",
      brief_preview,
      f"https://aifri.day/brief/{date_path}/",
      og_type="article",
      json_ld_blocks=[bc_json, article_json],
  )}
</head>
<body>
  <div class="container">
    {_nav("/brief/", "All Briefs")}
    {bc_html}
    <header class="brief-header">
      <h1 class="brief-date">{escaped_date}</h1>
    </header>
    <div class="brief-lede">{lede_html}</div>
    <div class="brief-content">
      {chr(10).join(sections_html)}
      <section class="brief-section quick-links">
        <h2>Also</h2>
        <ul>{quick_links_html}</ul>
      </section>
    </div>
    <section class="sources">
      <h2 class="sources-title">Today&rsquo;s Sources</h2>
      <ul class="sources-list">{sources_html}</ul>
    </section>
    <nav class="brief-nav">{prev_html}{next_html}</nav>
  </div>
</body>
</html>'''


def _render_item(item):
    via = item.get("via", "")
    via_html = f'<span class="item-via">{escape(via)}</span>' if via else ""
    return f'''<div class="item">
  <h3 class="item-title"><a href="{escape(item["url"])}">{escape(item["title"])}</a></h3>
  <div class="item-body">{item["body"]}</div>
  {via_html}
</div>'''


def _is_community_section(title):
    lower = title.lower()
    return "community" in lower or "slack" in lower


def _render_quick_link(ql):
    note = ql.get("note", "")
    note_html = f' &mdash; <span class="ql-note">{escape(note)}</span>' if note else ""
    return f'<li><a href="{escape(ql["url"])}">{escape(ql["title"])}</a>{note_html}</li>'


# ---------------------------------------------------------------------------
# /brief/ index
# ---------------------------------------------------------------------------

def render_index(briefs):
    """Render the /brief/ index page."""
    # Latest brief
    latest = max(briefs, key=lambda x: x["date_path"]) if briefs else None
    latest_html = ""
    if latest:
        lede_text = re.sub(r'<[^>]+>', '', latest.get("lede", ""))[:220]
        if len(lede_text) >= 220:
            lede_text = lede_text[:lede_text.rfind(' ')] + '\u2026'
        latest_html = f'''<div class="brief-latest-card">
  <div class="blc-label">Today&rsquo;s Brief</div>
  <a href="/brief/{latest['date_path']}/" class="blc-date">{escape(latest['date'])}</a>
  <p class="blc-preview">{escape(lede_text)}</p>
  <a href="/brief/{latest['date_path']}/" class="blc-cta">Read today&rsquo;s brief &rarr;</a>
</div>'''

    # Archive list (most recent first, skip today)
    archive_briefs = sorted(briefs, key=lambda x: x["date_path"], reverse=True)
    if latest:
        archive_briefs = [b for b in archive_briefs if b["date_path"] != latest["date_path"]]

    items = []
    for b in archive_briefs:
        lede_text = re.sub(r'<[^>]+>', '', b.get("lede", ""))[:200]
        if len(lede_text) >= 200:
            lede_text = lede_text[:lede_text.rfind(' ')] + '...'
        items.append(f'''<article class="index-item">
  <a href="/brief/{b['date_path']}/">
    <h2 class="index-date">{escape(b['date'])}</h2>
    <p class="index-preview">{escape(lede_text)}</p>
  </a>
</article>''')

    # Year links
    years = sorted(set(b["date_path"].split("/")[0] for b in briefs), reverse=True)
    year_links = " &middot; ".join(
        f'<a href="/brief/{y}/">{y}</a>' for y in years
    )

    sources_html = _sources_section_html()

    # JSON-LD
    item_list = {
        "@context": "https://schema.org",
        "@type": "ItemList",
        "name": "AI Friday Daily Briefs",
        "url": "https://aifri.day/brief/",
        "itemListElement": [
            {"@type": "ListItem", "position": i+1,
             "url": f"https://aifri.day/brief/{b['date_path']}/",
             "name": f"AI Friday Daily Brief — {b['date']}"}
            for i, b in enumerate(sorted(briefs, key=lambda x: x["date_path"], reverse=True)[:20])
        ]
    }
    bc_list = {
        "@context": "https://schema.org",
        "@type": "BreadcrumbList",
        "itemListElement": [
            {"@type": "ListItem", "position": 1, "name": "AI Friday", "item": "https://aifri.day/"},
            {"@type": "ListItem", "position": 2, "name": "Daily Brief", "item": "https://aifri.day/brief/"},
        ]
    }
    item_list_json = json.dumps(item_list, indent=2)
    bc_list_json = json.dumps(bc_list, indent=2)

    bc_html = _breadcrumbs([("AI Friday", "/"), ("Daily Brief", None)])

    return f'''<!DOCTYPE html>
<html lang="en">
<head>
  {HEAD_COMMON}
  {_head(
      "AI Friday — Daily Brief",
      "Curated AI news, tools, and analysis for builders — updated daily by the AI Friday community in New Orleans.",
      "https://aifri.day/brief/",
      json_ld_blocks=[item_list_json, bc_list_json],
  )}
</head>
<body>
  <div class="container">
    {_nav("/", "Home")}
    {bc_html}
    <header class="brief-index-header">
      <h1 class="brief-index-title">AI Friday<br>Daily Brief</h1>
      <p class="brief-index-desc">Curated AI news and tools for builders, published every weekday. We scan dozens of sources so you don&rsquo;t have to &mdash; then distill the signal into a focused morning read.</p>
      <p class="brief-index-years">Archive: {year_links}</p>
    </header>
    {latest_html}
    <div class="index-list">
      {chr(10).join(items)}
    </div>
    <section class="brief-sources-section">
      <h2 class="brief-sources-title">Where We Look</h2>
      <p class="brief-sources-desc">Every brief draws from these sources, plus links shared by the community in our Slack.</p>
      {sources_html}
    </section>
  </div>
</body>
</html>'''


# ---------------------------------------------------------------------------
# Year index: /brief/YYYY/
# ---------------------------------------------------------------------------

def render_year_index(year, months_with_days, brief_map):
    """Render /brief/YYYY/ — one mini-calendar per month."""
    calendars = []
    for month in sorted(months_with_days.keys(), reverse=True):
        days = set(months_with_days[month])
        month_name = MONTH_NAMES[int(month)]
        month_int = int(month)
        year_int = int(year)

        cal = calendar.monthcalendar(year_int, month_int)
        rows = ["<tr><th>Mo</th><th>Tu</th><th>We</th><th>Th</th><th>Fr</th><th>Sa</th><th>Su</th></tr>"]
        for week in cal:
            cells = []
            for day in week:
                if day == 0:
                    cells.append("<td></td>")
                else:
                    d = f"{day:02d}"
                    if d in days:
                        url = f"/brief/{year}/{month}/{d}/"
                        cells.append(f'<td class="cal-has-brief"><a href="{url}">{day}</a></td>')
                    else:
                        cells.append(f'<td class="cal-empty">{day}</td>')
            rows.append("<tr>" + "".join(cells) + "</tr>")

        # Month lede snippets (up to 3)
        snippets = []
        for d in sorted(days, reverse=True)[:3]:
            key = f"{year}/{month}/{d}"
            b = brief_map.get(key)
            if b:
                lede = re.sub(r'<[^>]+>', '', b.get('lede', ''))[:120]
                if len(lede) >= 120:
                    lede = lede[:lede.rfind(' ')] + '\u2026'
                snippets.append(f'<li><a href="/brief/{key}/">{escape(b["date"])}</a> &mdash; {escape(lede)}</li>')
        snippets_html = f'<ul class="year-month-snippets">{chr(10).join(snippets)}</ul>' if snippets else ""

        calendars.append(f'''<div class="year-month-block">
  <h2 class="ymb-title"><a href="/brief/{year}/{month}/">{month_name} {year}</a></h2>
  <table class="mini-cal">{chr(10).join(rows)}</table>
  {snippets_html}
</div>''')

    # All years for nav
    all_years_in_dir = sorted(
        [d.name for d in (SITE_DIR / "brief").iterdir() if d.is_dir() and d.name.isdigit()],
        reverse=True
    )
    year_nav = " &middot; ".join(
        f'<strong>{y}</strong>' if y == year else f'<a href="/brief/{y}/">{y}</a>'
        for y in all_years_in_dir
    )

    bc_json = json.dumps({
        "@context": "https://schema.org",
        "@type": "BreadcrumbList",
        "itemListElement": [
            {"@type": "ListItem", "position": 1, "name": "AI Friday", "item": "https://aifri.day/"},
            {"@type": "ListItem", "position": 2, "name": "Daily Brief", "item": "https://aifri.day/brief/"},
            {"@type": "ListItem", "position": 3, "name": year, "item": f"https://aifri.day/brief/{year}/"},
        ]
    }, indent=2)

    bc_html = _breadcrumbs([("AI Friday", "/"), ("Brief", "/brief/"), (year, None)])

    total_briefs = sum(len(d) for d in months_with_days.values())

    return f'''<!DOCTYPE html>
<html lang="en">
<head>
  {HEAD_COMMON}
  {_head(
      f"AI Friday Daily Brief — {year}",
      f"Browse {total_briefs} AI Friday daily briefs from {year}. Curated AI news, tools, and analysis for startup founders and builders.",
      f"https://aifri.day/brief/{year}/",
      json_ld=bc_json,
  )}
</head>
<body>
  <div class="container">
    {_nav("/brief/", "All Briefs")}
    {bc_html}
    <header class="brief-header">
      <h1 class="brief-date">{year}</h1>
      <p class="brief-theme">{total_briefs} brief{"s" if total_briefs != 1 else ""} &middot; {year_nav}</p>
    </header>
    <div class="year-grid">
      {chr(10).join(calendars)}
    </div>
  </div>
</body>
</html>'''


# ---------------------------------------------------------------------------
# Month index: /brief/YYYY/MM/
# ---------------------------------------------------------------------------

def render_month_index(year, month, days_with_data, brief_map, all_year_months=None):
    """Render /brief/YYYY/MM/ — full calendar + list of briefs."""
    month_name = MONTH_NAMES[int(month)]
    year_int, month_int = int(year), int(month)
    days_set = set(days_with_data)

    cal = calendar.monthcalendar(year_int, month_int)
    rows = ["<tr><th>Mon</th><th>Tue</th><th>Wed</th><th>Thu</th><th>Fri</th><th>Sat</th><th>Sun</th></tr>"]
    for week in cal:
        cells = []
        for day in week:
            if day == 0:
                cells.append('<td class="cal-pad"></td>')
            else:
                d = f"{day:02d}"
                if d in days_set:
                    url = f"/brief/{year}/{month}/{d}/"
                    cells.append(f'<td class="cal-has-brief"><a href="{url}">{day}</a></td>')
                else:
                    cells.append(f'<td class="cal-empty">{day}</td>')
        rows.append("<tr>" + "".join(cells) + "</tr>")
    cal_html = f'<table class="month-cal">{chr(10).join(rows)}</table>'

    # List of briefs this month
    items = []
    for d in sorted(days_with_data, reverse=True):
        key = f"{year}/{month}/{d}"
        b = brief_map.get(key)
        if b:
            lede_text = re.sub(r'<[^>]+>', '', b.get('lede', ''))[:200]
            if len(lede_text) >= 200:
                lede_text = lede_text[:lede_text.rfind(' ')] + '...'
            items.append(f'''<article class="index-item">
  <a href="/brief/{key}/">
    <h2 class="index-date">{escape(b['date'])}</h2>
    <p class="index-preview">{escape(lede_text)}</p>
  </a>
</article>''')

    # Prev/next month nav — only link to months that have briefs
    prev_m = month_int - 1
    prev_y = year_int
    if prev_m == 0:
        prev_m = 12
        prev_y -= 1
    next_m = month_int + 1
    next_y = year_int
    if next_m == 13:
        next_m = 1
        next_y += 1
    prev_key = f"{prev_y}/{prev_m:02d}"
    next_key = f"{next_y}/{next_m:02d}"
    has_prev = all_year_months and prev_key in all_year_months
    has_next = all_year_months and next_key in all_year_months
    prev_link = f'<a href="/brief/{prev_key}/" class="month-nav-prev">&larr; {MONTH_NAMES[prev_m]} {prev_y}</a>' if has_prev else '<span></span>'
    next_link = f'<a href="/brief/{next_key}/" class="month-nav-next">{MONTH_NAMES[next_m]} {next_y} &rarr;</a>' if has_next else '<span></span>'
    month_nav = f'''<div class="month-nav">
  {prev_link}
  {next_link}
</div>'''

    bc_json = json.dumps({
        "@context": "https://schema.org",
        "@type": "BreadcrumbList",
        "itemListElement": [
            {"@type": "ListItem", "position": 1, "name": "AI Friday", "item": "https://aifri.day/"},
            {"@type": "ListItem", "position": 2, "name": "Daily Brief", "item": "https://aifri.day/brief/"},
            {"@type": "ListItem", "position": 3, "name": year, "item": f"https://aifri.day/brief/{year}/"},
            {"@type": "ListItem", "position": 4, "name": f"{month_name} {year}", "item": f"https://aifri.day/brief/{year}/{month}/"},
        ]
    }, indent=2)

    bc_html = _breadcrumbs([
        ("AI Friday", "/"),
        ("Brief", "/brief/"),
        (year, f"/brief/{year}/"),
        (f"{month_name} {year}", None),
    ])

    total = len(days_with_data)

    return f'''<!DOCTYPE html>
<html lang="en">
<head>
  {HEAD_COMMON}
  {_head(
      f"AI Friday Daily Brief — {month_name} {year}",
      f"{total} AI Friday daily brief{"s" if total != 1 else ""} from {month_name} {year}. Curated AI news, tools, and analysis for startup founders and builders.",
      f"https://aifri.day/brief/{year}/{month}/",
      json_ld=bc_json,
  )}
</head>
<body>
  <div class="container">
    {_nav(f"/brief/{year}/", str(year))}
    {bc_html}
    <header class="brief-header">
      <h1 class="brief-date">{month_name} {year}</h1>
      <p class="brief-theme">{total} brief{"s" if total != 1 else ""} this month</p>
    </header>
    {cal_html}
    {month_nav}
    <div class="index-list" style="margin-top: var(--space-xl)">
      {chr(10).join(items)}
    </div>
  </div>
</body>
</html>'''


# ---------------------------------------------------------------------------
# Sitemap
# ---------------------------------------------------------------------------

def render_sitemap(briefs):
    urls = []
    urls.append("<url><loc>https://aifri.day/brief/</loc><changefreq>daily</changefreq><priority>0.9</priority></url>")

    years = {}
    for b in briefs:
        parts = b["date_path"].split("/")
        year, month, day = parts[0], parts[1], parts[2]
        years.setdefault(year, {})
        years[year].setdefault(month, [])
        years[year][month].append(day)

    for year in sorted(years.keys(), reverse=True):
        urls.append(f"<url><loc>https://aifri.day/brief/{year}/</loc><changefreq>monthly</changefreq><priority>0.7</priority></url>")
        for month in sorted(years[year].keys(), reverse=True):
            urls.append(f"<url><loc>https://aifri.day/brief/{year}/{month}/</loc><changefreq>monthly</changefreq><priority>0.7</priority></url>")

    for b in sorted(briefs, key=lambda x: x["date_path"], reverse=True):
        dp = b["date_path"]
        urls.append(f"<url><loc>https://aifri.day/brief/{dp}/</loc><lastmod>{dp.replace('/', '-')}</lastmod><changefreq>never</changefreq><priority>0.8</priority></url>")

    return ('<?xml version="1.0" encoding="UTF-8"?>\n'
            '<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">\n'
            + "\n".join(urls) + "\n</urlset>")


# ---------------------------------------------------------------------------
# Build all
# ---------------------------------------------------------------------------

def build_all(data_dir):
    data_path = Path(data_dir)
    briefs = []

    for f in sorted(data_path.glob("brief_*.json")):
        with open(f) as fh:
            txt = fh.read().strip()
            if txt.startswith('```'): txt = txt.split('\n', 1)[1]
            if txt.endswith('```'): txt = txt.rsplit('\n', 1)[0]
            try:
                data = json.loads(txt)
                briefs.append(data)
                print(f"  Loaded {f.name}: {data['date']}")
            except json.JSONDecodeError as e:
                print(f"  SKIP {f.name}: {e}")

    # Build a lookup map: date_path -> brief data
    brief_map = {b["date_path"]: b for b in briefs}

    # Generate individual brief pages
    for data in briefs:
        date_path = data["date_path"]
        out_dir = SITE_DIR / "brief" / date_path
        out_dir.mkdir(parents=True, exist_ok=True)
        html = render_brief(data)
        (out_dir / "index.html").write_text(html)
        print(f"  Generated brief/{date_path}/index.html")

    # Generate /brief/index.html
    (SITE_DIR / "brief").mkdir(parents=True, exist_ok=True)
    (SITE_DIR / "brief" / "index.html").write_text(render_index(briefs))
    print(f"  Generated brief/index.html ({len(briefs)} briefs)")

    # Build year/month structure
    years = {}
    for b in briefs:
        parts = b["date_path"].split("/")
        year, month, day = parts[0], parts[1], parts[2]
        years.setdefault(year, {})
        years[year].setdefault(month, [])
        years[year][month].append(day)

    # Build set of all year/month combos for prev/next nav
    all_year_months = set()
    for y, ms in years.items():
        for m in ms:
            all_year_months.add(f"{y}/{m}")

    for year, months in years.items():
        # Year index
        year_dir = SITE_DIR / "brief" / year
        year_dir.mkdir(parents=True, exist_ok=True)
        (year_dir / "index.html").write_text(render_year_index(year, months, brief_map))
        print(f"  Generated brief/{year}/index.html")

        # Month indices
        for month, days in months.items():
            month_dir = SITE_DIR / "brief" / year / month
            month_dir.mkdir(parents=True, exist_ok=True)
            (month_dir / "index.html").write_text(render_month_index(year, month, days, brief_map, all_year_months))
            print(f"  Generated brief/{year}/{month}/index.html")

    # Sitemap
    sitemap_path = SITE_DIR / "brief-sitemap.xml"
    sitemap_path.write_text(render_sitemap(briefs))
    print(f"  Generated brief-sitemap.xml")

    # Update homepage "Latest Brief" link
    if briefs:
        latest = max(briefs, key=lambda x: x["date_path"])
        home = SITE_DIR / "index.html"
        if home.exists():
            content = home.read_text()
            content = re.sub(
                r'href="/brief/[^"]*"(.*?)Latest Brief',
                f'href="/brief/{latest["date_path"]}/"\\1Latest Brief',
                content
            )
            home.write_text(content)
            print(f"  Updated homepage latest brief link -> {latest['date_path']}")


if __name__ == "__main__":
    data_dir = sys.argv[1] if len(sys.argv) > 1 else "/tmp"
    build_all(data_dir)
