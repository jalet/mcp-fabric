#!/usr/bin/env python3
"""Generate docs/architecture-diagram.{drawio,png} for MCP Fabric.

Self-contained system-context diagram (no external icon fetches, so the
draw.io CLI export is deterministic). Run:

    python3 docs/architecture-diagram.py

then convert the PNG to WebP:

    cwebp -q 90 docs/architecture-diagram.png -o docs/architecture-diagram.webp
"""

import os
import shutil
import subprocess
import xml.etree.ElementTree as ET


def export_png(drawio_path: str, scale: int = 2) -> str | None:
    """Export a .drawio file to PNG via the draw.io CLI (no-op if absent)."""
    drawio = shutil.which("drawio")
    if drawio is None:
        print("draw.io CLI not found - skipping PNG export")
        return None
    png = drawio_path.rsplit(".", 1)[0] + ".png"
    subprocess.run(
        [drawio, "--export", "--no-sandbox", "--format", "png",
         "--scale", str(scale), "--output", png, drawio_path],
        check=True, capture_output=True,
    )
    print(f"exported {png}")
    return png


BG = "#1E1E1E"

# Palette tuned for the dark background: (fill, stroke, font)
GREY = ("#2A2A2A", "#999999", "#E0E0E0")
BLUE = ("#152A3D", "#4C8EDA", "#9CC4F0")
PURPLE = ("#2A1A35", "#A06CD5", "#C9A6F0")
GREEN = ("#16261A", "#4CA86A", "#8FD3A0")
ORANGE = ("#2E2010", "#E0871F", "#FFC066")
TASK = ("#1A2535", "#5FA8E0", "#ABD2F5")


def _model():
    mxfile = ET.Element("mxfile")
    diagram = ET.SubElement(mxfile, "diagram", id="arch", name="MCP Fabric")
    model = ET.SubElement(
        diagram, "mxGraphModel",
        dx="1400", dy="900", grid="0", gridSize="10", guides="1", tooltips="1",
        connect="1", arrows="1", fold="1", page="1", pageScale="1",
        pageWidth="1380", pageHeight="820", math="0", shadow="0", background=BG,
    )
    root = ET.SubElement(model, "root")
    ET.SubElement(root, "mxCell", id="0")
    ET.SubElement(root, "mxCell", id="1", parent="0")
    return mxfile, root


def box(root, cid, label, x, y, w, h, palette, *, group=False, bold=False, fontsize=12):
    fill, stroke, font = palette
    style = (
        f"rounded=1;whiteSpace=wrap;html=1;fillColor={fill};strokeColor={stroke};"
        f"fontColor={font};fontSize={fontsize};labelBackgroundColor=none;"
    )
    if group:
        style += "verticalAlign=top;align=left;spacingLeft=12;spacingTop=8;fontStyle=1;dashed=1;"
    else:
        style += "align=center;verticalAlign=middle;"
    if bold:
        style += "fontStyle=1;"
    c = ET.SubElement(root, "mxCell", id=cid, value=label, style=style,
                      vertex="1", parent="1")
    ET.SubElement(c, "mxGeometry", x=str(x), y=str(y), width=str(w),
                  height=str(h)).set("as", "geometry")


def text(root, cid, label, x, y, w, h, color="#FFFFFF", size=20, bold=True):
    style = (f"text;html=1;align=center;verticalAlign=middle;fontColor={color};"
             f"fontSize={size};labelBackgroundColor=none;{'fontStyle=1;' if bold else ''}")
    c = ET.SubElement(root, "mxCell", id=cid, value=label, style=style,
                      vertex="1", parent="1")
    ET.SubElement(c, "mxGeometry", x=str(x), y=str(y), width=str(w),
                  height=str(h)).set("as", "geometry")


def edge(root, cid, src, dst, label="", *, dashed=False, color="#E0E0E0",
         exit_xy=None, entry_xy=None):
    style = (
        f"edgeStyle=orthogonalEdgeStyle;rounded=0;html=1;strokeColor={color};"
        f"strokeWidth=2;fontColor=#FFFFFF;fontSize=11;labelBackgroundColor=none;"
        f"endArrow=block;endFill=1;"
    )
    if dashed:
        style += "dashed=1;"
    if exit_xy:
        style += f"exitX={exit_xy[0]};exitY={exit_xy[1]};exitDx=0;exitDy=0;"
    if entry_xy:
        style += f"entryX={entry_xy[0]};entryY={entry_xy[1]};entryDx=0;entryDy=0;"
    c = ET.SubElement(root, "mxCell", id=cid, value=label, style=style,
                      edge="1", parent="1", source=src, target=dst)
    g = ET.SubElement(c, "mxGeometry", relative="1")
    g.set("as", "geometry")


mxfile, root = _model()

# Title
text(root, "title", "MCP Fabric — Kubernetes-Native AI Agent Platform",
     40, 16, 1300, 40, color="#FFFFFF", size=22)

# ── Clients ───────────────────────────────────────────────────────────────
box(root, "clients", "Clients", 380, 72, 600, 96, GREY, group=True)
box(root, "cli", "Claude Code CLI", 400, 104, 170, 52, GREY)
box(root, "apps", "Applications &amp; APIs", 585, 104, 180, 52, GREY)
box(root, "mcpc", "MCP Clients", 790, 104, 170, 52, GREY)

# ── Gateway namespace ───────────────────────────────────────────────────────
box(root, "gw_ns", "mcp-fabric-gateway namespace", 320, 220, 700, 120, BLUE, group=True)
box(root, "routes_cm", "routes ConfigMap\n(compiled from Route CRs)", 340, 256, 200, 64, BLUE)
box(root, "gateway", "Agent Gateway\n(Go HTTP service)", 700, 252, 300, 72, BLUE, bold=True)

# ── Operator (system namespace) ─────────────────────────────────────────────
box(root, "op_ns", "mcp-fabric-system namespace", 1060, 220, 300, 330, PURPLE, group=True)
box(root, "operator", "Operator\n(controller-runtime)", 1080, 256, 260, 48, PURPLE, bold=True)
box(root, "c_agent", "Agent controller", 1090, 322, 240, 40, PURPLE)
box(root, "c_route", "Route controller", 1090, 372, 240, 40, PURPLE)
box(root, "c_tool", "Tool controller", 1090, 422, 240, 40, PURPLE)
box(root, "c_task", "Task controller", 1090, 472, 240, 40, PURPLE)

# ── Agents namespace ────────────────────────────────────────────────────────
box(root, "ag_ns", "mcp-fabric-agents namespace", 40, 400, 1000, 210, GREEN, group=True)
box(root, "a_text", "text-assistant\nAgent + Service", 60, 438, 215, 64, GREEN)
box(root, "a_artist", "engineering-artist\nAgent + Service", 290, 438, 215, 64, GREEN)
box(root, "a_orch", "task-orchestrator\nAgent (orchestrator)", 520, 438, 215, 64, GREEN)
box(root, "a_worker", "code-worker\nAgent (worker, standalone=false)", 750, 438, 270, 64, GREEN)
box(root, "task_job",
    "Task Job  —  one Pod:  git-clone init  →  worker sidecar (HTTP :8080)  →  orchestrator   |   shared Workspace PVC",
    60, 524, 960, 60, TASK, bold=True)

# ── External services ───────────────────────────────────────────────────────
box(root, "ext", "External Services", 320, 648, 760, 110, ORANGE, group=True)
box(root, "bedrock", "AWS Bedrock\n(LLM, via IRSA)", 340, 684, 220, 60, ORANGE)
box(root, "mcp_srv", "MCP Servers\n(stdio / HTTP)", 585, 684, 220, 60, ORANGE)
box(root, "git", "Git (GitHub)\ncommit · push · PR", 830, 684, 230, 60, ORANGE)

# ── Edges ───────────────────────────────────────────────────────────────────
edge(root, "e_cli_gw", "clients", "gw_ns", "POST /v1/invoke",
     exit_xy=(0.5, 1), entry_xy=(0.5, 0))
edge(root, "e_gw_ag", "gw_ns", "ag_ns", "route to agent",
     exit_xy=(0.4, 1), entry_xy=(0.55, 0))
edge(root, "e_op_gw", "operator", "gateway", "compiles routes",
     dashed=True, color="#A06CD5", exit_xy=(0, 0.5), entry_xy=(1, 0.5))
edge(root, "e_op_ag", "op_ns", "ag_ns", "creates Deployments / Jobs / PVCs",
     dashed=True, color="#A06CD5", exit_xy=(0, 0.9), entry_xy=(1, 0.5))
edge(root, "e_ag_ext", "ag_ns", "ext", "LLM &amp; tool calls · git push / PR",
     exit_xy=(0.5, 1), entry_xy=(0.5, 0))

out = os.path.join("docs", "architecture-diagram.drawio")
ET.ElementTree(mxfile).write(out, encoding="utf-8", xml_declaration=True)
print(f"wrote {out}")
export_png(out, scale=2)
