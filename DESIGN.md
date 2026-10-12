---
version: alpha
colors:
  background: "#101b2a"
  surface: "#18283a"
  text: "#f0f5fc"
  muted: "#afc0d4"
  primary: "#a9d5ff"
  primaryHover: "#c8e6ff"
  border: "#40556c"
  danger: "#ffa9a2"
  scrollbarThumb: "#6885a4"
  scrollbarHover: "#90accb"
typography:
  body:
    fontFamily: "system-ui, -apple-system, BlinkMacSystemFont, Segoe UI, sans-serif"
    fontSize: "16px"
    lineHeight: "1.55"
  url:
    fontFamily: "ui-monospace, Cascadia Code, monospace"
rounded:
  surface: "12px"
  control: "7px"
spacing:
  section: "24px"
components:
  primaryButton:
    backgroundColor: "{colors.primary}"
    textColor: "{colors.background}"
---

## Overview

Waymark is a private link workspace. Its route-marker symbol makes the destination theme concrete; ink-blue surfaces and pale-blue accents carry the same identity through the app and documentation. A serif introduction gives the sign-in screen its voice; the operational workspace uses the established system sans. The rebrand replaces the earlier chain/green identity by the owner's request. Product register, English interface, no Japan-market workflow. No external fonts, motion-heavy decoration or telemetry jargon in user flows.

## Colors

`web/app.css :root` is the canonical runtime source. This document records its values: background→--bg, surface→--surface, text→--text, muted→--muted, primary→--accent, primaryHover→--accent-hover, border→--border, danger→--danger, scrollbarThumb→--thumb, scrollbarHover→--thumb-hover. Track inherits background. Change shared tokens and this document together. The pale-blue accent carries the Waymark identity; status also uses text so color is never the only cue.

## Typography

System sans for controls and content; Georgia/Times New Roman serif for the sign-in introduction only; monospace only for actual URLs/codes. Headings use sentence case. Long destinations wrap and remain available as links. No remote font requests or late font swaps.

## Layout

1120px maximum document width. Login pairs a short introduction with the form. The workspace places creation beside aggregate counts, followed by a paginated links table. At 760px the layout stacks; table overflow stays in its own region. Document owns vertical scroll.

## Elevation & Depth

Borders establish regions. Static panels have no decorative shadows. Modal backdrop dims the workspace without reflow.

## Shapes

12px surface radius, 7px controls. Status badges are compact pills. 44px minimum buttons; explicit keyboard focus.

## Components

One global control style, input/label pattern, feedback region and dialog. Canonical behavior is in UX-CONTRACT.md. Light theme is not offered. Dark/forced-color/reduced-motion states are supported. No native alerts or browser validation bubbles.

## Do's and Don'ts

Use real anchors for destinations, buttons for actions and textContent for user content. Show the full short link and preserve form entries on failures. Never expose passwords, invent analytics, or describe reversible disabling as permanent deletion.

## Iconography

A route-marker post is the brand symbol. The same geometry appears in the masthead, favicon and cover. README section icons use the workspace canonical outline family; product actions retain visible verb labels.

## Motion

No automatic entrance animation. Brief control feedback only; reduced-motion disables transitions.
