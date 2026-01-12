"""String manipulation tools using Strands @tool decorator."""

from strands import tool


@tool
def reverse_string(text: str) -> str:
    """Reverse a string.

    Args:
        text: The string to reverse

    Returns:
        The reversed string
    """
    return text[::-1]


@tool
def word_count(text: str) -> dict:
    """Count words and characters in text.

    Args:
        text: The text to analyze

    Returns:
        Dictionary with word count, character count, and character count without spaces
    """
    words = text.split()
    return {
        "words": len(words),
        "characters": len(text),
        "characters_no_spaces": len(text.replace(" ", "")),
        "lines": text.count("\n") + 1 if text else 0,
    }


@tool
def to_case(text: str, case: str = "upper") -> str:
    """Convert text to a different case.

    Args:
        text: The text to convert
        case: Target case - one of: upper, lower, title, capitalize, swapcase

    Returns:
        The converted text
    """
    case = case.lower()
    if case == "upper":
        return text.upper()
    elif case == "lower":
        return text.lower()
    elif case == "title":
        return text.title()
    elif case == "capitalize":
        return text.capitalize()
    elif case == "swapcase":
        return text.swapcase()
    else:
        return text


@tool
def replace_text(text: str, old: str, new: str, count: int = -1) -> str:
    """Replace occurrences of a substring.

    Args:
        text: The original text
        old: The substring to find
        new: The replacement string
        count: Maximum replacements (-1 for all)

    Returns:
        Text with replacements made
    """
    if count == -1:
        return text.replace(old, new)
    return text.replace(old, new, count)


@tool
def split_text(text: str, delimiter: str = " ", max_splits: int = -1) -> list[str]:
    """Split text by a delimiter.

    Args:
        text: The text to split
        delimiter: The delimiter to split on (default: space)
        max_splits: Maximum number of splits (-1 for unlimited)

    Returns:
        List of split parts
    """
    if max_splits == -1:
        return text.split(delimiter)
    return text.split(delimiter, max_splits)
