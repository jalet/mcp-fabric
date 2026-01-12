"""Dynamic tool loader for ToolPackages.

This module loads @tool decorated functions from ToolPackage OCI images
that have been extracted by init containers to /tools/.

ToolPackage modules should export tools via __all__ in __init__.py:

    # string_tools/__init__.py
    from .tools import reverse_string, word_count, to_case
    __all__ = ["reverse_string", "word_count", "to_case"]
"""

import importlib
import logging
from typing import Callable

logger = logging.getLogger(__name__)


def load_tools_from_config(config: dict) -> list[Callable]:
    """Load @tool decorated functions from ToolPackages in config.

    Uses __all__ from the module to discover tools (standard Python convention).

    Args:
        config: Agent configuration dict containing toolPackages list

    Returns:
        List of callable tool functions
    """
    tools = []

    for tp in config.get("toolPackages", []):
        entry_module = tp.get("entryModule")
        enabled = set(tp.get("enabledTools", []))
        disabled = set(tp.get("disabledTools", []))
        pkg_name = tp.get("name", entry_module)

        if not entry_module:
            logger.warning(f"toolPackage {pkg_name} has no entryModule, skipping")
            continue

        try:
            logger.info(f"loading toolPackage {pkg_name} from {entry_module}")
            module = importlib.import_module(entry_module)

            # Use __all__ if defined, otherwise get all public names
            tool_names = getattr(module, "__all__", None)
            if tool_names is None:
                tool_names = [n for n in dir(module) if not n.startswith("_")]
                logger.debug(f"no __all__ defined, found {len(tool_names)} public names")

            loaded_count = 0
            for name in tool_names:
                # Apply enabled/disabled filters
                if enabled and name not in enabled:
                    logger.debug(f"skipping {name}: not in enabledTools")
                    continue
                if name in disabled:
                    logger.debug(f"skipping {name}: in disabledTools")
                    continue

                obj = getattr(module, name, None)
                if obj and callable(obj):
                    tools.append(obj)
                    loaded_count += 1
                    logger.info(f"  loaded tool: {name}")

            logger.info(f"loaded {loaded_count} tools from {pkg_name}")

        except ImportError as e:
            logger.error(f"failed to import {entry_module}: {e}")
            logger.error(f"  make sure the toolPackage image contains the module at /app/{entry_module.replace('.', '/')}")

    logger.info(f"total tools loaded from toolPackages: {len(tools)}")
    return tools
