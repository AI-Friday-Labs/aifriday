#!/usr/bin/env python3
"""Generate static HTML for all briefs and index pages."""
import json
import os
import re
import sys
from html import escape
from pathlib import Path

SITE_DIR = Path(__file__).parent / "site"

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

    # SEO metadata
    brief_preview_raw = re.sub(r'<[^>]+>', '', lede_html)[:160]
    if len(brief_preview_raw) >= 160:
        brief_preview_raw = brief_preview_raw[:brief_preview_raw.rfind(' ')] + '…'
    brief_preview = escape(brief_preview_raw)
    date_path = data["date_path"]
    escaped_date = escape(data["date"])
    iso_date = date_path.replace('/', '-')
    json_ld = json.dumps({
        "@context": "https://schema.org",
        "@type": "Article",
        "headline": f"AI Friday Brief — {data['date']}",
        "datePublished": iso_date,
        "url": f"https://aifri.day/brief/{date_path}/",
        "publisher": {
            "@type": "Organization",
            "name": "AI Friday",
            "url": "https://aifri.day"
        }
    }, indent=2)

    return f'''<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>AI Friday &mdash; {escaped_date}</title>
  <meta name="description" content="{brief_preview}">
  <link rel="canonical" href="https://aifri.day/brief/{date_path}/">
  <link rel="alternate" type="application/rss+xml" title="AI Friday Daily Brief" href="https://aifri.day/feed.xml">
  <meta property="og:title" content="AI Friday Brief — {escaped_date}">
  <meta property="og:description" content="{brief_preview}">
  <meta property="og:url" content="https://aifri.day/brief/{date_path}/">
  <meta property="og:type" content="article">
  <meta property="og:site_name" content="AI Friday">
  <meta name="twitter:card" content="summary">
  <meta name="twitter:title" content="AI Friday Brief — {escaped_date}">
  <meta name="twitter:description" content="{brief_preview}">
  <link rel="preconnect" href="https://fonts.googleapis.com">
  <link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
  <link href="https://fonts.googleapis.com/css2?family=Crimson+Text:wght@400;600;700&family=Instrument+Sans:wght@400;500;600;700&family=JetBrains+Mono:wght@400;500&display=swap" rel="stylesheet">
  <link rel="stylesheet" href="/static/style.css">
  <link rel="icon" href="/favicon.ico" sizes="48x48">
  <link rel="icon" href="/icon-192.png" type="image/png" sizes="192x192">
  <link rel="apple-touch-icon" href="/apple-touch-icon.png">
  <script defer src="https://umami-production-d337.up.railway.app/script.js" data-website-id="3a8001f6-6312-473a-b2bd-38dae609847c"></script>
  <script src="https://analytics.ahrefs.com/analytics.js" data-key="23HVW+pAWhX3mLQMX/nA5A" async></script>
  <script type="application/ld+json">
  {json_ld}
  </script>
</head>
<body>
  <div class="container">
    <nav class="nav">
      <a href="/" class="nav-logo"><span class="logo-ai">AI</span><span class="logo-friday">&thinsp;Friday</span></a>
      <div class="nav-links"><a href="/brief/" class="nav-back">&larr; All Briefs</a></div>
    </nav>
    <header class="brief-header">
      <h1 class="brief-date">{escape(data["date"])}</h1>
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
    <footer class="footer">
      <span class="footer-fleur">&#9884;&#65039;</span>
      New Orleans, LA
    </footer>
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
    """Check if a section title indicates community-sourced content."""
    lower = title.lower()
    return "community" in lower or "slack" in lower


def _render_quick_link(ql):
    note = ql.get("note", "")
    note_html = f' &mdash; <span class="ql-note">{escape(note)}</span>' if note else ""
    return f'<li><a href="{escape(ql["url"])}">{escape(ql["title"])}</a>{note_html}</li>'


def render_index(briefs):
    """Render the /brief/ index page listing all briefs."""
    items = []
    for b in sorted(briefs, key=lambda x: x["date_path"], reverse=True):
        lede_text = b.get("lede", "").replace("<strong>", "").replace("</strong>", "")
        # Strip HTML for preview
        preview = re.sub(r'<[^>]+>', '', lede_text)[:200]
        if len(preview) >= 200:
            preview = preview[:preview.rfind(' ')] + '...'
        items.append(f'''<article class="index-item">
  <a href="/brief/{b["date_path"]}/">
    <h2 class="index-date">{escape(b["date"])}</h2>
    <p class="index-preview">{preview}</p>
  </a>
</article>''')

    return f'''<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>AI Friday &mdash; Daily Briefs</title>
  <meta name="description" content="Curated AI news and tools for builders. Updated daily by the AI Friday community in New Orleans.">
  <link rel="canonical" href="https://aifri.day/brief/">
  <link rel="alternate" type="application/rss+xml" title="AI Friday Daily Brief" href="https://aifri.day/feed.xml">
  <meta property="og:title" content="AI Friday — Daily Briefs">
  <meta property="og:description" content="Curated AI news and tools for builders. Updated daily.">
  <meta property="og:url" content="https://aifri.day/brief/">
  <meta property="og:type" content="website">
  <meta property="og:site_name" content="AI Friday">
  <meta name="twitter:card" content="summary">
  <meta name="twitter:title" content="AI Friday — Daily Briefs">
  <meta name="twitter:description" content="Curated AI news and tools for builders. Updated daily.">
  <link rel="preconnect" href="https://fonts.googleapis.com">
  <link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
  <link href="https://fonts.googleapis.com/css2?family=Crimson+Text:wght@400;600;700&family=Instrument+Sans:wght@400;500;600;700&family=JetBrains+Mono:wght@400;500&display=swap" rel="stylesheet">
  <link rel="stylesheet" href="/static/style.css">
  <link rel="icon" href="/favicon.ico" sizes="48x48">
  <link rel="icon" href="/icon-192.png" type="image/png" sizes="192x192">
  <link rel="apple-touch-icon" href="/apple-touch-icon.png">
  <script defer src="https://umami-production-d337.up.railway.app/script.js" data-website-id="3a8001f6-6312-473a-b2bd-38dae609847c"></script>
  <script src="https://analytics.ahrefs.com/analytics.js" data-key="23HVW+pAWhX3mLQMX/nA5A" async></script>
</head>
<body>
  <div class="container">
    <nav class="nav">
      <a href="/" class="nav-logo"><span class="logo-ai">AI</span><span class="logo-friday">&thinsp;Friday</span></a>
      <div class="nav-links"><a href="/" class="nav-back">&larr; Home</a></div>
    </nav>
    <header class="brief-header">
      <h1 class="brief-date">Daily Briefs</h1>
      <p class="brief-theme">Curated AI news and tools for builders. Updated daily.</p>
    </header>
    <div class="index-list">
      {chr(10).join(items)}
    </div>
    <footer class="footer">
      <span class="footer-fleur">&#9884;&#65039;</span>
      New Orleans, LA
    </footer>
  </div>
</body>
</html>'''


def render_year_index(year, months_with_days):
    """Render /brief/2026/ index."""
    items = []
    for month, days in sorted(months_with_days.items(), reverse=True):
        month_name = ["", "January", "February", "March", "April", "May", "June",
                       "July", "August", "September", "October", "November", "December"][int(month)]
        day_links = " &middot; ".join(
            f'<a href="/brief/{year}/{month}/{d}/">{d}</a>'
            for d in sorted(days, reverse=True)
        )
        items.append(f'''<div class="year-month">
  <h2>{month_name} {year}</h2>
  <p class="year-days">{day_links}</p>
</div>''')

    return f'''<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>AI Friday &mdash; {year} Briefs</title>
  <meta name="description" content="AI Friday daily briefs from {year}. Curated AI news for builders.">
  <link rel="canonical" href="https://aifri.day/brief/{year}/">
  <meta property="og:title" content="AI Friday — {year} Briefs">
  <meta property="og:description" content="AI Friday daily briefs from {year}.">
  <meta property="og:url" content="https://aifri.day/brief/{year}/">
  <meta property="og:type" content="website">
  <meta property="og:site_name" content="AI Friday">
  <link rel="preconnect" href="https://fonts.googleapis.com">
  <link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
  <link href="https://fonts.googleapis.com/css2?family=Crimson+Text:wght@400;600;700&family=Instrument+Sans:wght@400;500;600;700&family=JetBrains+Mono:wght@400;500&display=swap" rel="stylesheet">
  <link rel="stylesheet" href="/static/style.css">
  <link rel="icon" href="/favicon.ico" sizes="48x48">
  <link rel="icon" href="/icon-192.png" type="image/png" sizes="192x192">
  <link rel="apple-touch-icon" href="/apple-touch-icon.png">
  <script defer src="https://umami-production-d337.up.railway.app/script.js" data-website-id="3a8001f6-6312-473a-b2bd-38dae609847c"></script>
  <script src="https://analytics.ahrefs.com/analytics.js" data-key="23HVW+pAWhX3mLQMX/nA5A" async></script>
</head>
<body>
  <div class="container">
    <nav class="nav">
      <a href="/" class="nav-logo"><span class="logo-ai">AI</span><span class="logo-friday">&thinsp;Friday</span></a>
      <div class="nav-links"><a href="/brief/" class="nav-back">&larr; All Briefs</a></div>
    </nav>
    <header class="brief-header">
      <h1 class="brief-date">{year}</h1>
    </header>
    <div class="brief-content">
      {chr(10).join(items)}
    </div>
    <footer class="footer">
      <span class="footer-fleur">&#9884;&#65039;</span>
      New Orleans, LA
    </footer>
  </div>
</body>
</html>'''


def build_all(data_dir):
    """Build all HTML from JSON files in data_dir."""
    data_path = Path(data_dir)
    briefs = []

    # Load all brief JSON files
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

    # Generate year index pages
    years = {}
    for b in briefs:
        parts = b["date_path"].split("/")
        year, month, day = parts[0], parts[1], parts[2]
        if year not in years:
            years[year] = {}
        if month not in years[year]:
            years[year][month] = []
        years[year][month].append(day)

    for year, months in years.items():
        year_dir = SITE_DIR / "brief" / year
        year_dir.mkdir(parents=True, exist_ok=True)
        (year_dir / "index.html").write_text(render_year_index(year, months))
        print(f"  Generated brief/{year}/index.html")

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
