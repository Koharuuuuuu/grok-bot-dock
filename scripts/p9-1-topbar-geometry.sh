#!/usr/bin/env bash
set -euo pipefail

: "${PLAYWRIGHT_CLI:?Set PLAYWRIGHT_CLI to the installed playwright_cli.sh wrapper}"

for width in 1024 1280 1366 1440 1600 1839 1920 768 800 801 820 853 900 901 960 720 640 320 360 375 390 414 430; do
  "$PLAYWRIGHT_CLI" resize "$width" 900 >/dev/null
  result=$("$PLAYWRIGHT_CLI" eval '() => {
    const width = innerWidth;
    const errors = [];
    const rect = selector => document.querySelector(selector)?.getBoundingClientRect();
    const header = rect(".topbar");
    const primary = rect(".top-primary-controls");
    const secondary = rect(".top-secondary-controls");
    const select = rect(".source-select-wrap select");
    const sourceWrap = rect(".source-select-wrap");
    const theme = rect(".theme-trigger");
    if (!header || !primary || !secondary || !select || !theme) errors.push("missing-control");
    if (document.documentElement.scrollWidth > width) errors.push("page-horizontal-overflow");
    if (width >= 801) {
      const actions = rect(".top-actions");
      if (actions.height > 64) errors.push("desktop-actions-too-tall");
      if (Math.abs(primary.y - secondary.y) > 3) errors.push("desktop-groups-not-horizontal");
      if (actions.right > width) errors.push("desktop-actions-outside-viewport");
      const stats = [...document.querySelectorAll(".top-secondary-controls .stat")]
        .filter(item => getComputedStyle(item).display !== "none");
      if (stats.length < 2) errors.push("desktop-stats-hidden");
      for (const item of stats) {
        const box = item.getBoundingClientRect();
        if (box.width < 30 || box.height > 50) errors.push("stat-column-compressed");
        if (getComputedStyle(item.querySelector(".stat-value")).display === "none") errors.push("stat-value-hidden");
        if (getComputedStyle(item.querySelector(".stat-label")).display === "none") errors.push("stat-label-hidden");
      }
    }
    if (width >= 541 && width < 801) {
      const actions = rect(".top-actions");
      if (Math.abs(primary.y - secondary.y) > 3) errors.push("tablet-groups-not-horizontal");
      if (actions.right > width || actions.width < primary.width + secondary.width) errors.push("tablet-actions-outside-viewport");
    }
    if (width < 541 && (select.height < 40 || theme.width < 40 || Math.abs(select.y - theme.y) > 3)) errors.push("mobile-primary-controls-invalid");
    if (width >= 541 && (select.width < 132 || sourceWrap.width > 250 || getComputedStyle(document.querySelector(".source-select-wrap select")).textOverflow !== "ellipsis" || theme.width < 30)) errors.push("desktop-controls-invalid");
    for (const [selector, box] of [["source", select], ["theme", theme]]) {
      const hit = document.elementFromPoint(box.x + box.width / 2, box.y + box.height / 2);
      if (!hit || !(hit.matches(selector === "source" ? ".source-select-wrap select" : ".theme-trigger") || hit.closest(selector === "source" ? ".source-select-wrap" : ".theme-control"))) errors.push(`${selector}-not-clickable`);
    }
    if (errors.length) throw new Error(`${width}px: ${errors.join(",")}`);
    return JSON.stringify({status:"PASS", width, headerHeight:Math.round(header.height), primaryWidth:Math.round(primary.width), secondaryWidth:Math.round(secondary.width)});
  }')
  if [[ "$result" != *'PASS'* ]]; then
    printf 'FAIL %spx: %s\n' "$width" "$result" >&2
    exit 1
  fi
  printf 'PASS %spx\n' "$width"
done
