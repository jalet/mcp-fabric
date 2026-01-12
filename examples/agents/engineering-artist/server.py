#!/usr/bin/env python3
"""Engineering Artist agent with native draw.io diagram generation tools."""

import json
import os
import logging
import time
import uuid
import zlib
import base64
from urllib.parse import quote
from flask import Flask, request, jsonify
from strands import Agent, tool
from strands.models import BedrockModel

LOG_FORMAT = "[%(asctime)s] [%(process)d] [%(levelname)s] %(message)s"
DATE_FORMAT = "%Y-%m-%d %H:%M:%S %z"
logging.basicConfig(level=logging.INFO, format=LOG_FORMAT, datefmt=DATE_FORMAT)
logger = logging.getLogger("engineering-artist")

app = Flask(__name__)

config = {}


# In-memory diagram state management
class DiagramState:
    """Manages diagram sessions with components and connections."""

    def __init__(self):
        self.diagrams: dict[str, dict] = {}

    def create(self, diagram_id: str, title: str, diagram_type: str, style: str) -> None:
        """Create a new diagram."""
        self.diagrams[diagram_id] = {
            "id": diagram_id,
            "title": title,
            "type": diagram_type,
            "style": style,
            "components": {},
            "connections": [],
            "groups": {},
            "next_id": 2,  # 0 and 1 are reserved for root cells
        }

    def get(self, diagram_id: str) -> dict | None:
        """Get diagram by ID."""
        return self.diagrams.get(diagram_id)

    def add_component(self, diagram_id: str, component: dict) -> str:
        """Add component and return its ID."""
        diagram = self.diagrams.get(diagram_id)
        if not diagram:
            return ""
        comp_id = str(diagram["next_id"])
        diagram["next_id"] += 1
        component["id"] = comp_id
        diagram["components"][comp_id] = component
        return comp_id

    def add_connection(self, diagram_id: str, connection: dict) -> str:
        """Add connection and return its ID."""
        diagram = self.diagrams.get(diagram_id)
        if not diagram:
            return ""
        conn_id = str(diagram["next_id"])
        diagram["next_id"] += 1
        connection["id"] = conn_id
        diagram["connections"].append(connection)
        return conn_id

    def add_group(self, diagram_id: str, group: dict) -> str:
        """Add group and return its ID."""
        diagram = self.diagrams.get(diagram_id)
        if not diagram:
            return ""
        group_id = str(diagram["next_id"])
        diagram["next_id"] += 1
        group["id"] = group_id
        diagram["groups"][group_id] = group
        return group_id


diagram_state = DiagramState()


# Style presets for different cloud providers
STYLE_PRESETS = {
    "default": {
        "service": "rounded=1;whiteSpace=wrap;html=1;fillColor=#dae8fc;strokeColor=#6c8ebf;",
        "database": "shape=cylinder3;whiteSpace=wrap;html=1;boundedLbl=1;backgroundOutline=1;size=15;fillColor=#d5e8d4;strokeColor=#82b366;",
        "queue": "shape=parallelogram;perimeter=parallelogramPerimeter;whiteSpace=wrap;html=1;fillColor=#fff2cc;strokeColor=#d6b656;",
        "api": "rounded=1;whiteSpace=wrap;html=1;fillColor=#e1d5e7;strokeColor=#9673a6;",
        "user": "shape=umlActor;verticalLabelPosition=bottom;verticalAlign=top;html=1;outlineConnect=0;",
        "cloud": "ellipse;shape=cloud;whiteSpace=wrap;html=1;fillColor=#f5f5f5;strokeColor=#666666;",
        "container": "rounded=0;whiteSpace=wrap;html=1;fillColor=#f8cecc;strokeColor=#b85450;",
        "server": "rounded=0;whiteSpace=wrap;html=1;fillColor=#d5e8d4;strokeColor=#82b366;",
        "group": "rounded=1;whiteSpace=wrap;html=1;fillColor=none;strokeColor=#666666;dashed=1;",
    },
    "aws": {
        "service": "rounded=1;whiteSpace=wrap;html=1;fillColor=#FF9900;strokeColor=#232F3E;fontColor=#232F3E;",
        "database": "shape=cylinder3;whiteSpace=wrap;html=1;boundedLbl=1;backgroundOutline=1;size=15;fillColor=#3B48CC;strokeColor=#232F3E;fontColor=#ffffff;",
        "queue": "rounded=1;whiteSpace=wrap;html=1;fillColor=#FF4F8B;strokeColor=#232F3E;fontColor=#232F3E;",
        "api": "rounded=1;whiteSpace=wrap;html=1;fillColor=#E7157B;strokeColor=#232F3E;fontColor=#ffffff;",
        "container": "rounded=1;whiteSpace=wrap;html=1;fillColor=#ED7100;strokeColor=#232F3E;fontColor=#232F3E;",
        "server": "rounded=1;whiteSpace=wrap;html=1;fillColor=#ED7100;strokeColor=#232F3E;fontColor=#232F3E;",
        "group": "rounded=1;whiteSpace=wrap;html=1;fillColor=#E6F2FF;strokeColor=#147EBA;dashed=0;",
    },
    "azure": {
        "service": "rounded=1;whiteSpace=wrap;html=1;fillColor=#0078D4;strokeColor=#0078D4;fontColor=#ffffff;",
        "database": "shape=cylinder3;whiteSpace=wrap;html=1;boundedLbl=1;backgroundOutline=1;size=15;fillColor=#0078D4;strokeColor=#0078D4;fontColor=#ffffff;",
        "queue": "rounded=1;whiteSpace=wrap;html=1;fillColor=#54AEF0;strokeColor=#0078D4;fontColor=#000000;",
        "api": "rounded=1;whiteSpace=wrap;html=1;fillColor=#50E6FF;strokeColor=#0078D4;fontColor=#000000;",
        "group": "rounded=1;whiteSpace=wrap;html=1;fillColor=#E6F7FF;strokeColor=#0078D4;dashed=0;",
    },
    "gcp": {
        "service": "rounded=1;whiteSpace=wrap;html=1;fillColor=#4285F4;strokeColor=#4285F4;fontColor=#ffffff;",
        "database": "shape=cylinder3;whiteSpace=wrap;html=1;boundedLbl=1;backgroundOutline=1;size=15;fillColor=#34A853;strokeColor=#34A853;fontColor=#ffffff;",
        "queue": "rounded=1;whiteSpace=wrap;html=1;fillColor=#FBBC04;strokeColor=#FBBC04;fontColor=#000000;",
        "api": "rounded=1;whiteSpace=wrap;html=1;fillColor=#EA4335;strokeColor=#EA4335;fontColor=#ffffff;",
        "group": "rounded=1;whiteSpace=wrap;html=1;fillColor=#E8F0FE;strokeColor=#4285F4;dashed=0;",
    },
    "kubernetes": {
        "service": "rounded=1;whiteSpace=wrap;html=1;fillColor=#326CE5;strokeColor=#326CE5;fontColor=#ffffff;",
        "database": "shape=cylinder3;whiteSpace=wrap;html=1;boundedLbl=1;backgroundOutline=1;size=15;fillColor=#326CE5;strokeColor=#326CE5;fontColor=#ffffff;",
        "container": "rounded=1;whiteSpace=wrap;html=1;fillColor=#326CE5;strokeColor=#326CE5;fontColor=#ffffff;",
        "group": "rounded=1;whiteSpace=wrap;html=1;fillColor=#E6F0FF;strokeColor=#326CE5;dashed=0;",
    },
    "minimal": {
        "service": "rounded=1;whiteSpace=wrap;html=1;fillColor=#ffffff;strokeColor=#000000;",
        "database": "shape=cylinder3;whiteSpace=wrap;html=1;boundedLbl=1;backgroundOutline=1;size=15;fillColor=#ffffff;strokeColor=#000000;",
        "queue": "shape=parallelogram;perimeter=parallelogramPerimeter;whiteSpace=wrap;html=1;fillColor=#ffffff;strokeColor=#000000;",
        "api": "rounded=1;whiteSpace=wrap;html=1;fillColor=#ffffff;strokeColor=#000000;",
        "group": "rounded=1;whiteSpace=wrap;html=1;fillColor=none;strokeColor=#000000;dashed=1;",
    },
}

# Architecture templates
TEMPLATES = {
    "three-tier": {
        "components": [
            {"type": "user", "label": "Users", "x": 400, "y": 50},
            {"type": "api", "label": "Load Balancer", "x": 400, "y": 150},
            {"type": "service", "label": "Web Server 1", "x": 250, "y": 250},
            {"type": "service", "label": "Web Server 2", "x": 550, "y": 250},
            {"type": "service", "label": "App Server 1", "x": 250, "y": 350},
            {"type": "service", "label": "App Server 2", "x": 550, "y": 350},
            {"type": "database", "label": "Primary DB", "x": 300, "y": 450},
            {"type": "database", "label": "Replica DB", "x": 500, "y": 450},
        ],
        "connections": [
            {"source": 0, "target": 1, "label": "HTTPS"},
            {"source": 1, "target": 2, "label": ""},
            {"source": 1, "target": 3, "label": ""},
            {"source": 2, "target": 4, "label": ""},
            {"source": 3, "target": 5, "label": ""},
            {"source": 4, "target": 6, "label": "SQL"},
            {"source": 5, "target": 7, "label": "SQL"},
            {"source": 6, "target": 7, "label": "Replication", "style": "dashed"},
        ],
    },
    "microservices": {
        "components": [
            {"type": "api", "label": "API Gateway", "x": 400, "y": 100},
            {"type": "service", "label": "User Service", "x": 150, "y": 250},
            {"type": "service", "label": "Order Service", "x": 350, "y": 250},
            {"type": "service", "label": "Product Service", "x": 550, "y": 250},
            {"type": "service", "label": "Payment Service", "x": 750, "y": 250},
            {"type": "queue", "label": "Message Queue", "x": 400, "y": 400},
            {"type": "database", "label": "User DB", "x": 150, "y": 500},
            {"type": "database", "label": "Order DB", "x": 350, "y": 500},
            {"type": "database", "label": "Product DB", "x": 550, "y": 500},
        ],
        "connections": [
            {"source": 0, "target": 1, "label": "REST"},
            {"source": 0, "target": 2, "label": "REST"},
            {"source": 0, "target": 3, "label": "REST"},
            {"source": 0, "target": 4, "label": "REST"},
            {"source": 2, "target": 5, "label": "Publish"},
            {"source": 4, "target": 5, "label": "Subscribe"},
            {"source": 1, "target": 6, "label": ""},
            {"source": 2, "target": 7, "label": ""},
            {"source": 3, "target": 8, "label": ""},
        ],
    },
    "event-driven": {
        "components": [
            {"type": "service", "label": "Producer 1", "x": 150, "y": 100},
            {"type": "service", "label": "Producer 2", "x": 150, "y": 250},
            {"type": "queue", "label": "Event Bus", "x": 400, "y": 175},
            {"type": "service", "label": "Consumer 1", "x": 650, "y": 100},
            {"type": "service", "label": "Consumer 2", "x": 650, "y": 250},
            {"type": "database", "label": "Event Store", "x": 400, "y": 350},
        ],
        "connections": [
            {"source": 0, "target": 2, "label": "Events"},
            {"source": 1, "target": 2, "label": "Events"},
            {"source": 2, "target": 3, "label": "Subscribe"},
            {"source": 2, "target": 4, "label": "Subscribe"},
            {"source": 2, "target": 5, "label": "Persist"},
        ],
    },
    "data-pipeline": {
        "components": [
            {"type": "database", "label": "Source DB", "x": 100, "y": 200},
            {"type": "service", "label": "Extract", "x": 250, "y": 200},
            {"type": "service", "label": "Transform", "x": 400, "y": 200},
            {"type": "service", "label": "Load", "x": 550, "y": 200},
            {"type": "database", "label": "Data Warehouse", "x": 700, "y": 200},
            {"type": "service", "label": "Analytics", "x": 700, "y": 350},
        ],
        "connections": [
            {"source": 0, "target": 1, "label": "Read"},
            {"source": 1, "target": 2, "label": "Raw Data"},
            {"source": 2, "target": 3, "label": "Clean Data"},
            {"source": 3, "target": 4, "label": "Write"},
            {"source": 4, "target": 5, "label": "Query"},
        ],
    },
}


def generate_mxgraph_xml(diagram: dict) -> str:
    """Generate draw.io compatible mxGraph XML from diagram state."""
    style_preset = STYLE_PRESETS.get(diagram["style"], STYLE_PRESETS["default"])

    cells = []

    # Root cells (always present)
    cells.append('<mxCell id="0"/>')
    cells.append('<mxCell id="1" parent="0"/>')

    # Add groups first (so components can be inside them)
    for group_id, group in diagram.get("groups", {}).items():
        group_style = style_preset.get("group", STYLE_PRESETS["default"]["group"])
        x, y = group.get("x", 50), group.get("y", 50)
        width, height = group.get("width", 300), group.get("height", 200)
        cells.append(
            f'<mxCell id="{group_id}" value="{group["label"]}" '
            f'style="{group_style}" vertex="1" parent="1">'
            f'<mxGeometry x="{x}" y="{y}" width="{width}" height="{height}" as="geometry"/>'
            f"</mxCell>"
        )

    # Add components
    for comp_id, comp in diagram.get("components", {}).items():
        comp_type = comp.get("type", "service")
        style = style_preset.get(comp_type, style_preset.get("service"))
        x, y = comp.get("x", 100), comp.get("y", 100)
        width, height = comp.get("width", 120), comp.get("height", 60)
        parent = comp.get("parent", "1")
        cells.append(
            f'<mxCell id="{comp_id}" value="{comp["label"]}" '
            f'style="{style}" vertex="1" parent="{parent}">'
            f'<mxGeometry x="{x}" y="{y}" width="{width}" height="{height}" as="geometry"/>'
            f"</mxCell>"
        )

    # Add connections
    for conn in diagram.get("connections", []):
        conn_id = conn.get("id", str(uuid.uuid4())[:8])
        source_id = conn.get("source_id")
        target_id = conn.get("target_id")
        label = conn.get("label", "")
        line_style = conn.get("line_style", "solid")
        arrow_style = conn.get("arrow_style", "single")

        style_parts = ["edgeStyle=orthogonalEdgeStyle", "rounded=0", "html=1"]
        if line_style == "dashed":
            style_parts.append("dashed=1")
        elif line_style == "dotted":
            style_parts.append("dashed=1;dashPattern=1 2")
        if arrow_style == "double":
            style_parts.append("startArrow=classic;startFill=1")
        elif arrow_style == "none":
            style_parts.append("endArrow=none;endFill=0")

        style = ";".join(style_parts) + ";"

        cells.append(
            f'<mxCell id="{conn_id}" value="{label}" style="{style}" '
            f'edge="1" parent="1" source="{source_id}" target="{target_id}">'
            f"<mxGeometry relative=\"1\" as=\"geometry\"/>"
            f"</mxCell>"
        )

    # Build complete XML
    xml = f"""<?xml version="1.0" encoding="UTF-8"?>
<mxfile host="app.diagrams.net" modified="{time.strftime('%Y-%m-%dT%H:%M:%S.000Z')}" agent="Engineering Artist" version="1.0">
  <diagram id="{diagram['id']}" name="{diagram['title']}">
    <mxGraphModel dx="1000" dy="600" grid="1" gridSize="10" guides="1" tooltips="1" connect="1" arrows="1" fold="1" page="1" pageScale="1" pageWidth="850" pageHeight="1100">
      <root>
        {"".join(cells)}
      </root>
    </mxGraphModel>
  </diagram>
</mxfile>"""
    return xml


def compress_xml_for_drawio(xml: str) -> str:
    """Compress XML to draw.io URL-safe format."""
    compressed = zlib.compress(xml.encode("utf-8"), 9)
    b64 = base64.b64encode(compressed).decode("utf-8")
    return quote(b64, safe="")


def load_config():
    """Load agent configuration from mounted ConfigMap."""
    global config
    config_path = os.environ.get("AGENT_CONFIG_PATH", "/etc/agent/config/agent.json")
    try:
        with open(config_path) as f:
            config = json.load(f)
        logger.info(f"loaded config from {config_path}")
        return True
    except Exception as e:
        logger.error(f"could not load config: {e}")
        return False


# Native tools for diagram generation


@tool
def create_diagram(
    diagram_type: str,
    title: str,
    description: str,
    style: str = "default",
) -> str:
    """
    Create a new draw.io diagram.

    Args:
        diagram_type: Type of diagram (architecture, sequence, flowchart, network, deployment, data-flow, er)
        title: Title of the diagram
        description: Detailed description of what the diagram should show
        style: Visual style preset (default, aws, azure, gcp, kubernetes, minimal)

    Returns:
        diagram_id for use with other tools, plus confirmation message
    """
    diagram_id = str(uuid.uuid4())[:8]
    diagram_state.create(diagram_id, title, diagram_type, style)
    logger.info(f"created diagram {diagram_id}: {title} ({diagram_type}, {style})")

    return f"""Created diagram '{title}' with ID: {diagram_id}
Type: {diagram_type}
Style: {style}
Description: {description}

Use this diagram_id with add_component, add_connection, add_group, and export_diagram tools.
Available component types: service, database, queue, api, user, cloud, container, server
Available styles: default, aws, azure, gcp, kubernetes, minimal"""


@tool
def add_component(
    diagram_id: str,
    component_type: str,
    label: str,
    x: int = 100,
    y: int = 100,
    width: int = 120,
    height: int = 60,
    parent_group_id: str = "",
) -> str:
    """
    Add a component/shape to an existing diagram.

    Args:
        diagram_id: ID of the diagram to modify
        component_type: Type of component (service, database, queue, api, user, cloud, container, server)
        label: Label text for the component
        x: X position coordinate
        y: Y position coordinate
        width: Component width (default 120)
        height: Component height (default 60)
        parent_group_id: Optional group ID if component should be inside a group

    Returns:
        component_id for use with add_connection
    """
    diagram = diagram_state.get(diagram_id)
    if not diagram:
        return f"Error: Diagram '{diagram_id}' not found"

    component = {
        "type": component_type,
        "label": label,
        "x": x,
        "y": y,
        "width": width,
        "height": height,
    }
    if parent_group_id:
        component["parent"] = parent_group_id

    comp_id = diagram_state.add_component(diagram_id, component)
    logger.info(f"added {component_type} '{label}' (id={comp_id}) to diagram {diagram_id}")

    return f"Added {component_type} '{label}' with ID: {comp_id}"


@tool
def add_connection(
    diagram_id: str,
    source_id: str,
    target_id: str,
    label: str = "",
    line_style: str = "solid",
    arrow_style: str = "single",
) -> str:
    """
    Add a connection/arrow between two components.

    Args:
        diagram_id: ID of the diagram to modify
        source_id: ID of the source component
        target_id: ID of the target component
        label: Label for the connection (e.g., "HTTPS", "gRPC", "async")
        line_style: Line style (solid, dashed, dotted)
        arrow_style: Arrow style (single, double, none)

    Returns:
        connection_id
    """
    diagram = diagram_state.get(diagram_id)
    if not diagram:
        return f"Error: Diagram '{diagram_id}' not found"

    connection = {
        "source_id": source_id,
        "target_id": target_id,
        "label": label,
        "line_style": line_style,
        "arrow_style": arrow_style,
    }

    conn_id = diagram_state.add_connection(diagram_id, connection)
    logger.info(f"added connection {source_id} -> {target_id} (id={conn_id}) to diagram {diagram_id}")

    return f"Added connection from {source_id} to {target_id} with ID: {conn_id}"


@tool
def add_group(
    diagram_id: str,
    group_type: str,
    label: str,
    x: int = 50,
    y: int = 50,
    width: int = 300,
    height: int = 200,
) -> str:
    """
    Create a grouping container (e.g., VPC, namespace, region).

    Args:
        diagram_id: ID of the diagram to modify
        group_type: Type of group (vpc, subnet, region, namespace, cluster, zone, boundary)
        label: Label for the group
        x: X position coordinate
        y: Y position coordinate
        width: Group width
        height: Group height

    Returns:
        group_id for use as parent_group_id in add_component
    """
    diagram = diagram_state.get(diagram_id)
    if not diagram:
        return f"Error: Diagram '{diagram_id}' not found"

    group = {
        "type": group_type,
        "label": label,
        "x": x,
        "y": y,
        "width": width,
        "height": height,
    }

    group_id = diagram_state.add_group(diagram_id, group)
    logger.info(f"added {group_type} group '{label}' (id={group_id}) to diagram {diagram_id}")

    return f"Added {group_type} group '{label}' with ID: {group_id}. Use this ID as parent_group_id when adding components inside this group."


@tool
def export_diagram(
    diagram_id: str,
    output_format: str = "xml",
) -> str:
    """
    Export the diagram to a specific format.

    Args:
        diagram_id: ID of the diagram to export
        output_format: Export format (xml, drawio_url)

    Returns:
        Diagram content in requested format
    """
    diagram = diagram_state.get(diagram_id)
    if not diagram:
        return f"Error: Diagram '{diagram_id}' not found"

    xml = generate_mxgraph_xml(diagram)

    if output_format == "xml":
        logger.info(f"exported diagram {diagram_id} as XML")
        return f"```xml\n{xml}\n```"
    elif output_format == "drawio_url":
        compressed = compress_xml_for_drawio(xml)
        url = f"https://app.diagrams.net/#R{compressed}"
        logger.info(f"exported diagram {diagram_id} as draw.io URL")
        return f"Open this URL in your browser to view/edit the diagram:\n{url}"
    else:
        return f"Error: Unsupported format '{output_format}'. Use 'xml' or 'drawio_url'"


@tool
def apply_template(
    template_name: str,
    title: str = "",
    style: str = "default",
) -> str:
    """
    Apply a predefined architecture template.

    Args:
        template_name: Name of the template (three-tier, microservices, event-driven, data-pipeline)
        title: Optional custom title (defaults to template name)
        style: Visual style preset (default, aws, azure, gcp, kubernetes, minimal)

    Returns:
        diagram_id and summary of created components
    """
    template = TEMPLATES.get(template_name)
    if not template:
        available = ", ".join(TEMPLATES.keys())
        return f"Error: Template '{template_name}' not found. Available: {available}"

    diagram_id = str(uuid.uuid4())[:8]
    diagram_title = title or f"{template_name.replace('-', ' ').title()} Architecture"
    diagram_state.create(diagram_id, diagram_title, "architecture", style)

    # Add components
    component_ids = {}
    for i, comp in enumerate(template["components"]):
        component = {
            "type": comp["type"],
            "label": comp["label"],
            "x": comp["x"],
            "y": comp["y"],
            "width": 120,
            "height": 60,
        }
        comp_id = diagram_state.add_component(diagram_id, component)
        component_ids[i] = comp_id

    # Add connections
    for conn in template["connections"]:
        connection = {
            "source_id": component_ids[conn["source"]],
            "target_id": component_ids[conn["target"]],
            "label": conn.get("label", ""),
            "line_style": conn.get("style", "solid"),
            "arrow_style": "single",
        }
        diagram_state.add_connection(diagram_id, connection)

    logger.info(f"applied template {template_name} to diagram {diagram_id}")

    components_summary = ", ".join(c["label"] for c in template["components"])
    return f"""Applied '{template_name}' template.
Diagram ID: {diagram_id}
Title: {diagram_title}
Style: {style}
Components: {components_summary}

Use export_diagram to get the XML or draw.io URL."""


@tool
def generate_from_yaml(
    yaml_content: str,
    source_type: str,
    title: str = "",
    style: str = "kubernetes",
) -> str:
    """
    Generate a diagram from infrastructure-as-code YAML.

    Args:
        yaml_content: The YAML content to parse and visualize
        source_type: Type of YAML (kubernetes, docker-compose)
        title: Optional custom title
        style: Visual style preset

    Returns:
        diagram_id and summary of parsed resources
    """
    import yaml as yaml_parser

    try:
        docs = list(yaml_parser.safe_load_all(yaml_content))
    except Exception as e:
        return f"Error parsing YAML: {e}"

    diagram_id = str(uuid.uuid4())[:8]
    diagram_title = title or f"{source_type.title()} Infrastructure"
    diagram_state.create(diagram_id, diagram_title, "deployment", style)

    resources = []
    y_offset = 100

    for doc in docs:
        if not doc:
            continue

        if source_type == "kubernetes":
            kind = doc.get("kind", "Unknown")
            name = doc.get("metadata", {}).get("name", "unnamed")
            label = f"{kind}: {name}"

            comp_type = "container" if kind in ["Pod", "Deployment", "StatefulSet", "DaemonSet"] else "service"
            if kind in ["Service"]:
                comp_type = "api"
            elif kind in ["ConfigMap", "Secret"]:
                comp_type = "database"

            component = {
                "type": comp_type,
                "label": label,
                "x": 200,
                "y": y_offset,
                "width": 180,
                "height": 60,
            }
            diagram_state.add_component(diagram_id, component)
            resources.append(label)
            y_offset += 80

        elif source_type == "docker-compose":
            services = doc.get("services", {})
            for svc_name, svc_config in services.items():
                image = svc_config.get("image", "custom")
                label = f"{svc_name}\n({image})"

                component = {
                    "type": "container",
                    "label": label,
                    "x": 200,
                    "y": y_offset,
                    "width": 180,
                    "height": 60,
                }
                diagram_state.add_component(diagram_id, component)
                resources.append(svc_name)
                y_offset += 80

    logger.info(f"generated diagram {diagram_id} from {source_type} YAML with {len(resources)} resources")

    return f"""Generated diagram from {source_type} YAML.
Diagram ID: {diagram_id}
Title: {diagram_title}
Resources found: {len(resources)}
- {chr(10).join('- ' + r for r in resources[:10])}{'...' if len(resources) > 10 else ''}

Use export_diagram to get the XML or draw.io URL.
Use add_connection to add relationships between resources."""


def create_agent():
    """Create Strands agent with diagram tools."""
    model_config = config.get("model", {})
    model = BedrockModel(
        model_id=model_config.get("modelId", "eu.anthropic.claude-3-7-sonnet-20250219-v1:0"),
        max_tokens=model_config.get("maxTokens", 8192),
    )

    tools = [
        create_diagram,
        add_component,
        add_connection,
        add_group,
        export_diagram,
        apply_template,
        generate_from_yaml,
    ]

    return Agent(
        model=model,
        system_prompt=config.get("prompt", "You are an architecture diagram specialist."),
        tools=tools,
    )


# Initialize on startup
if load_config():
    logger.info("engineering artist agent initialized with diagram generation tools")


@app.route("/healthz")
def healthz():
    """Health check endpoint."""
    return jsonify({
        "status": "ok",
        "active_diagrams": len(diagram_state.diagrams),
        "available_templates": list(TEMPLATES.keys()),
        "available_styles": list(STYLE_PRESETS.keys()),
    })


@app.route("/invoke", methods=["POST"])
def invoke():
    """Handle agent invocation."""
    request_id = request.headers.get("X-Request-ID", str(uuid.uuid4())[:8])
    start_time = time.time()

    client_ip = request.remote_addr
    logger.info(f"[{request_id}] incoming request from {client_ip}")

    data = request.get_json() or {}
    query = data.get("query", "")
    tenant_id = data.get("tenantId", "unknown")
    correlation_id = data.get("correlationId", request_id)

    if not query:
        logger.warning(f"[{request_id}] missing query field")
        return jsonify({
            "success": False,
            "error": "Missing 'query' field",
        }), 400

    query_preview = query[:100].replace('\n', ' ') + "..." if len(query) > 100 else query
    logger.info(f"[{request_id}] tenant={tenant_id} correlation={correlation_id} query={query_preview}")

    try:
        agent = create_agent()
        logger.info(f"[{request_id}] invoking agent...")
        response = agent(query)

        elapsed = time.time() - start_time
        model_id = config.get("model", {}).get("modelId", "unknown")
        response_preview = str(response)[:100].replace('\n', ' ') + "..." if len(str(response)) > 100 else str(response)
        logger.info(f"[{request_id}] completed in {elapsed:.2f}s, response={response_preview}")

        return jsonify({
            "success": True,
            "result": {
                "response": str(response),
                "model": model_id,
            },
        })
    except Exception as e:
        elapsed = time.time() - start_time
        logger.error(f"[{request_id}] failed after {elapsed:.2f}s: {e}")

        return jsonify({
            "success": False,
            "error": str(e),
        }), 500


if __name__ == "__main__":
    app.run(host="0.0.0.0", port=8080)
