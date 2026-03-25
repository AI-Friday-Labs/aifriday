#!/usr/bin/env python3
"""Generate static HTML pages from brief JSON data and templates."""
import json
import sys
import os
from html import escape

def load_template(path):
    with open(path) as f:
        return f.read()

def render_brief(data, template):
    """Render brief data into the Go-template-style HTML, but with Python."""
    items_html = []
    for item in data["items"]:
        items_html.append(f'''      <article class="brief-card">
        <div class="brief-card-header">
          <span class="brief-card-emoji">{item["emoji"]}</span>
          <h3 class="brief-card-title">
            <a href="{escape(item["url"])}" target="_blank" rel="noopener">{escape(item["title"])}</a>
          </h3>
        </div>
        <div class="brief-card-body">
          <p class="brief-card-description">{escape(item["description"])}</p>
          <p class="brief-card-why">{escape(item["why"])}</p>
        </div>
      </article>''')

    sources_html = []
    for src in data.get("sources", []):
        sources_html.append(
            f'        <li><a href="{escape(src["url"])}" target="_blank" rel="noopener">{escape(src["name"])}</a></li>'
        )

    prev_date = data.get("prev_date", "")
    next_date = data.get("next_date", "")

    prev_html = f'''      <a href="/brief/{prev_date}" class="brief-nav-link brief-nav-link--prev">
        <span class="brief-nav-label">← Previous</span>
        <span class="brief-nav-date">{prev_date}</span>
      </a>''' if prev_date else '      <div class="brief-nav-placeholder"></div>'

    next_html = f'''      <a href="/brief/{next_date}" class="brief-nav-link brief-nav-link--next">
        <span class="brief-nav-label">Next →</span>
        <span class="brief-nav-date">{next_date}</span>
      </a>''' if next_date else '      <div class="brief-nav-placeholder"></div>'

    theme_html = f'      <p class="brief-theme">{escape(data["theme"])}</p>' if data.get("theme") else ""

    html = f'''<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>AI Friday Brief — {escape(data["date"])}</title>
  <meta name="description" content="AI Friday daily brief for {escape(data["date"])}. Curated AI news and tools for builders.">
  <link rel="preconnect" href="https://fonts.googleapis.com">
  <link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
  <link href="https://fonts.googleapis.com/css2?family=Fraunces:ital,opsz,wght@0,9..144,300;0,9..144,400;0,9..144,500;0,9..144,600;0,9..144,700;0,9..144,800;1,9..144,400;1,9..144,500&family=Plus+Jakarta+Sans:ital,wght@0,300;0,400;0,500;0,600;0,700;1,400&family=JetBrains+Mono:wght@400;500&display=swap" rel="stylesheet">
  <link rel="stylesheet" href="/static/style.css">
</head>
<body>
  <div class="container">
    <nav class="nav">
      <a href="/" class="nav-logo">
        <span class="logo-ai">AI</span><span class="logo-friday">&thinsp;Friday</span>
      </a>
      <div class="nav-links">
        <a href="/" class="nav-back">← Home</a>
      </div>
    </nav>

    <header class="brief-header">
      <h1 class="brief-date">{escape(data["date"])}</h1>
{theme_html}
    </header>

    <section class="brief-items">
{chr(10).join(items_html)}
    </section>

    <section class="sources">
      <h2 class="sources-title">Today's Sources</h2>
      <ul class="sources-list">
{chr(10).join(sources_html)}
      </ul>
    </section>

    <nav class="brief-nav">
{prev_html}
{next_html}
    </nav>

    <footer class="footer">
      <span class="footer-fleur">⚜️</span>
      New Orleans, LA
    </footer>
  </div>
</body>
</html>'''
    return html

if __name__ == "__main__":
    data_file = sys.argv[1]
    with open(data_file) as f:
        data = json.load(f)

    date_path = data["date_path"]  # e.g. "2026/03/25"
    out_dir = f"site/brief/{date_path}"
    os.makedirs(out_dir, exist_ok=True)

    html = render_brief(data, None)
    out_path = f"{out_dir}/index.html"
    with open(out_path, "w") as f:
        f.write(html)
    print(f"✅ Generated {out_path}")
