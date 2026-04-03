#!/usr/bin/env python3
"""Parse newsletter emails from Maildir and extract article links/summaries.

Reads .eml files from ~/Maildir/new/, extracts article links and descriptions,
skipping internal/management links. Outputs JSON to stdout.

Usage:
    python3 parse_newsletters.py              # JSON output
    python3 parse_newsletters.py --summary    # summary table only
"""

import email
import email.policy
import json
import os
import re
import sys
from html.parser import HTMLParser


MAILDIR = os.path.expanduser("~/Maildir/new")

# ---------- Skip patterns ----------

# Subjects that indicate welcome/confirm emails (case-insensitive substrings)
SKIP_SUBJECT_PATTERNS = [
    "welcome",
    "confirm your s",
    "confirm your email",
    "confirm signup",
    "verify your",
    "quick steps to get started",
    "we've got ai treats",
    "access to the ai-income",
]

# Senders to always skip
SKIP_SENDERS = [
    "support@exe.dev",
]

# URL patterns for internal/management links to skip
SKIP_URL_PATTERNS = [
    r"unsubscribe",
    r"manage.preferences",
    r"email.preferences",
    r"email[_-]preferences",
    r"subscription[_-]?preferences",
    r"manage[_-]?subscription",
    r"update[_-]?profile",
    r"complete.your.profile",
    r"upgrade.subscription",
    r"refer.a.friend",
    r"beehiivstatus\.com",
    r"(?:twitter|x)\.com/(?:intent|share)",
    r"facebook\.com/sharer",
    r"linkedin\.com/sharing",
    r"mailto:",
    r"^#$",
    r"^#[a-z]",
    r"terms.of.service",
    r"privacy.policy",
    r"advertise",
    r"become.a.sponsor",
    # Social profile links (not shares of articles)
    r"twitter\.com/[a-zA-Z0-9_]+$",
    r"x\.com/[a-zA-Z0-9_]+$",
    r"instagram\.com/[a-zA-Z0-9_]+/?$",
    r"facebook\.com/[a-zA-Z0-9_.]+/?$",
    r"linkedin\.com/(?:company|in)/",
    r"tiktok\.com/@",
    r"youtube\.com/(?:@|channel/)",
    r"threads\.net/",
]

# Link text patterns to skip (case-insensitive)
SKIP_TEXT_PATTERNS = [
    r"^unsubscribe",
    r"^manage\s+(?:your\s+)?(?:email\s+)?preferences",
    r"^update\s+(?:your\s+)?profile",
    r"^complete\s+(?:your\s+)?profile",
    r"^upgrade\s+(?:your\s+)?subscription",
    r"^refer\s+a\s+friend",
    r"^view\s+(?:in|this\s+email\s+in)\s+(?:your\s+)?browser",
    r"^view\s+online",
    r"^terms\s+of\s+service",
    r"^privacy\s+policy",
    r"^email\s+preferences?",
    r"^here$",  # generic "click here" for preferences
    r"^become\s+a\s+sponsor",
    r"^advertise",
    r"^subscribe$",
    r"^sign\s*up$",
    r"^share$",
    r"^tweet$",
    r"^download\s+the\s+app",
    r"^get\s+the\s+app",
]


# ---------- HTML Parser for link extraction ----------

class LinkExtractor(HTMLParser):
    """Extract <a> links with surrounding context from HTML."""

    def __init__(self):
        super().__init__()
        self.links = []  # list of {url, raw_text, context_before, context_after}
        self._current_link = None
        self._current_text = []
        self._all_text = []  # running text buffer for context
        self._in_a = False
        self._skip_tags = {"style", "script", "head", "title"}
        self._skip_depth = 0

    def handle_starttag(self, tag, attrs):
        if tag in self._skip_tags:
            self._skip_depth += 1
            return
        if self._skip_depth:
            return
        if tag == "a":
            href = None
            for name, val in attrs:
                if name == "href" and val:
                    href = val.strip()
            if href:
                self._in_a = True
                self._current_link = href
                self._current_text = []
        elif tag in ("br", "hr"):
            self._all_text.append(" ")
        elif tag in ("p", "div", "tr", "td", "li", "h1", "h2", "h3", "h4", "h5", "h6"):
            self._all_text.append("\n")

    def handle_endtag(self, tag):
        if tag in self._skip_tags and self._skip_depth:
            self._skip_depth -= 1
            return
        if self._skip_depth:
            return
        if tag == "a" and self._in_a:
            text = " ".join("".join(self._current_text).split()).strip()
            context = " ".join("".join(self._all_text[-200:]).split()).strip()
            self.links.append({
                "url": self._current_link,
                "raw_text": text,
                "context_before": context[-300:] if context else "",
            })
            # Add link text to running context
            self._all_text.append(text + " ")
            self._in_a = False
            self._current_link = None
            self._current_text = []

    def handle_data(self, data):
        if self._skip_depth:
            return
        if self._in_a:
            self._current_text.append(data)
        else:
            self._all_text.append(data)

    def handle_entityref(self, name):
        self.handle_data(" ")

    def handle_charref(self, name):
        self.handle_data(" ")


def get_context_after(html, links_data):
    """Second pass: extract text after each link for description context."""
    # We'll do a simpler regex-based approach for after-context
    for link in links_data:
        url_escaped = re.escape(link["url"][:40])  # partial match
        # Find the </a> after this href and grab following text
        pattern = re.compile(
            r'href=[\'"]' + url_escaped + r'.*?</a>\s*(.{0,500})',
            re.DOTALL | re.IGNORECASE
        )
        m = pattern.search(html)
        if m:
            after = m.group(1)
            # Strip HTML tags
            after_text = re.sub(r'<[^>]+>', ' ', after)
            after_text = ' '.join(after_text.split())[:200].strip()
            link["context_after"] = after_text
        else:
            link["context_after"] = ""


def should_skip_link(url, text):
    """Return True if this link should be filtered out."""
    url_lower = url.lower()
    text_lower = text.lower().strip()

    # Skip empty text links that are just images/tracking pixels
    if not text_lower:
        return True

    # Skip URL patterns
    for pat in SKIP_URL_PATTERNS:
        if re.search(pat, url_lower):
            return True

    # Skip text patterns
    for pat in SKIP_TEXT_PATTERNS:
        if re.search(pat, text_lower):
            return True

    return False


def should_skip_email(sender, subject):
    """Return True if this email should be skipped entirely."""
    sender_lower = sender.lower()
    subject_lower = subject.lower()

    # Skip specific senders
    for s in SKIP_SENDERS:
        if s in sender_lower:
            return True

    # Skip welcome/confirm subjects
    for pat in SKIP_SUBJECT_PATTERNS:
        if pat in subject_lower:
            return True

    return False


def extract_source_name(from_header):
    """Extract a clean source name from the From header."""
    # Try to get the display name
    m = re.match(r'"?([^"<]+?)"?\s*<', from_header)
    if m:
        name = m.group(1).strip()
        # Normalize known sources
        name_lower = name.lower()
        if "ben" in name_lower and "bites" in name_lower:
            return "Ben's Bites"
        if "taaft" in name_lower or "there's an ai" in name_lower:
            return "TAAFT - There's An AI For That"
        if "futuretools" in name_lower:
            return "FutureTools"
        if "tldr" in name_lower:
            return "TLDR AI"
        if "neuron" in name_lower:
            return "The Neuron"
        if "rundown" in name_lower:
            return "The Rundown AI"
        if "lenny" in name_lower:
            return "Lenny's Newsletter"
        if "daily brief" in name_lower or "nathaniel" in name_lower:
            return "AI Daily Brief"
        return name
    # Fallback: use email address
    m = re.search(r'<([^>]+)>', from_header)
    if m:
        return m.group(1)
    return from_header


def get_email_body(msg):
    """Extract HTML body (preferred) or text body from email message."""
    if msg.is_multipart():
        html_body = None
        text_body = None
        for part in msg.walk():
            ct = part.get_content_type()
            if ct == "text/html" and html_body is None:
                try:
                    html_body = part.get_content()
                except Exception:
                    try:
                        html_body = part.get_payload(decode=True).decode("utf-8", errors="replace")
                    except Exception:
                        pass
            elif ct == "text/plain" and text_body is None:
                try:
                    text_body = part.get_content()
                except Exception:
                    try:
                        text_body = part.get_payload(decode=True).decode("utf-8", errors="replace")
                    except Exception:
                        pass
        return html_body or text_body or "", html_body is not None
    else:
        ct = msg.get_content_type()
        try:
            body = msg.get_content()
        except Exception:
            try:
                body = msg.get_payload(decode=True).decode("utf-8", errors="replace")
            except Exception:
                body = ""
        return body, ct == "text/html"


def extract_links_from_html(html):
    """Extract and filter article links from HTML body."""
    parser = LinkExtractor()
    try:
        parser.feed(html)
    except Exception:
        pass

    # Get after-context
    get_context_after(html, parser.links)

    results = []
    seen_urls = set()

    for link_data in parser.links:
        url = link_data["url"]
        text = link_data["raw_text"]

        if should_skip_link(url, text):
            continue

        # Deduplicate by URL (keep first occurrence with text)
        # Normalize URL for dedup (strip tracking params roughly)
        url_key = url.split("?")[0] if "?utm" in url else url
        if url_key in seen_urls:
            continue
        seen_urls.add(url_key)

        # Build description from context
        ctx_after = link_data.get("context_after", "")
        # Trim description to something useful
        description = ctx_after[:200].strip() if ctx_after else ""

        results.append({
            "title": text,
            "url": url,
            "description": description,
        })

    return results


def extract_links_from_text(text):
    """Extract links from plain text body."""
    # Find URLs in text
    url_pattern = re.compile(r'https?://[^\s<>"\')]+', re.IGNORECASE)
    results = []
    seen = set()

    for m in url_pattern.finditer(text):
        url = m.group(0).rstrip(".,;:)")
        if url in seen:
            continue
        if should_skip_link(url, url):
            continue
        seen.add(url)

        # Get surrounding text for context
        start = max(0, m.start() - 100)
        end = min(len(text), m.end() + 200)
        context = text[start:end].strip()

        results.append({
            "title": url.split("/")[-1][:60] or url,
            "url": url,
            "description": context[:200],
        })

    return results


def parse_maildir(maildir_path):
    """Parse all emails in the maildir and extract newsletter data."""
    newsletters = []

    if not os.path.isdir(maildir_path):
        print(f"Error: {maildir_path} not found", file=sys.stderr)
        sys.exit(1)

    for filename in sorted(os.listdir(maildir_path)):
        if not filename.endswith(".eml"):
            continue

        filepath = os.path.join(maildir_path, filename)
        try:
            with open(filepath, "rb") as fp:
                msg = email.message_from_binary_file(fp, policy=email.policy.default)
        except Exception as e:
            print(f"Warning: could not parse {filename}: {e}", file=sys.stderr)
            continue

        sender = str(msg.get("From", ""))
        subject = str(msg.get("Subject", ""))

        if should_skip_email(sender, subject):
            print(f"  Skipping: {subject[:60]}", file=sys.stderr)
            continue

        source = extract_source_name(sender)
        body, is_html = get_email_body(msg)

        if not body:
            print(f"  Warning: empty body in {filename}", file=sys.stderr)
            continue

        if is_html:
            links = extract_links_from_html(body)
        else:
            links = extract_links_from_text(body)

        newsletters.append({
            "source": source,
            "subject": subject,
            "links": links,
        })

    return newsletters


def main():
    summary_mode = "--summary" in sys.argv

    newsletters = parse_maildir(MAILDIR)

    if summary_mode:
        print(f"\n{'Source':<35s} {'Subject':<45s} {'Links':>5s}")
        print("-" * 87)
        total = 0
        for nl in newsletters:
            count = len(nl["links"])
            total += count
            subj = nl["subject"][:43]
            print(f"{nl['source']:<35s} {subj:<45s} {count:>5d}")
        print("-" * 87)
        print(f"{'TOTAL':<81s} {total:>5d}")
        print(f"\n{len(newsletters)} newsletters processed")
    else:
        print(json.dumps(newsletters, indent=2, ensure_ascii=False))


if __name__ == "__main__":
    main()
