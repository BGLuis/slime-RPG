from typing import Any, Callable, Dict, Iterable, Iterator, List, Optional, Tuple
from src.extractor.Segment import Segment


class EventCommandStrategy:
    """Classe base unificada para estratégias de extração e inserção de comandos de evento (RPG Maker, WOLF RPG, etc.)."""

    kind: Optional[str] = None

    def extract(self, item: Dict[str, Any], context: Optional[Dict[str, Any]] = None) -> Any:
        return None

    def insert(self, item: Dict[str, Any], translated_text: Any, context: Optional[Dict[str, Any]] = None) -> None:
        pass

    def extract_segments(
        self,
        item: Dict[str, Any],
        prefix: str,
        file_name: str = "",
        context: Optional[Dict[str, Any]] = None,
    ) -> List[Segment]:
        val = self.extract(item, context=context)
        if not val:
            return []
        raw_code = item.get("code") if isinstance(item, dict) else None
        resolved_kind = (context and context.get("kind")) or getattr(self, "kind", None)
        if not resolved_kind:
            raise ValueError(f"Strategy {type(self).__name__} requires an explicit 'kind'")
        base_ctx = {"file": file_name, "code": raw_code, "kind": resolved_kind}
        if context:
            base_ctx.update(context)

        segments: List[Segment] = []
        if isinstance(val, str):
            segments.append(Segment(id=prefix, source=val, context=dict(base_ctx)))
        elif isinstance(val, list):
            for idx, sub_val in enumerate(val):
                if isinstance(sub_val, str):
                    ctx = dict(base_ctx)
                    ctx["index"] = idx
                    segments.append(Segment(id=f"{prefix}/{idx}", source=sub_val, context=ctx))
        elif isinstance(val, dict):
            for k, sub_val in val.items():
                if isinstance(sub_val, str):
                    ctx = dict(base_ctx)
                    ctx["key"] = k
                    segments.append(Segment(id=f"{prefix}/{k}", source=sub_val, context=ctx))
        return segments


def process_event_command_items(
    cmd_items: Iterable[Dict[str, Any]],
    prefix_base: str,
    base_name: str,
    cmd_lookup_fn: Callable[[int], Dict[str, Any]],
    strategy_lookup_fn: Callable[[Any], Optional[EventCommandStrategy]],
    kind_lookup_fn: Callable[[Any], str],
    mode: str = "extract",
    translations: Optional[Dict[str, str]] = None,
    segments: Optional[List[Segment]] = None,
) -> List[Dict[str, Any]]:
    """Itera comandos de evento em modo 'extract' ou 'apply', montando Segments ou atualizando valores traduzidos."""
    modified_items: List[Dict[str, Any]] = []
    translations = translations or {}

    for list_item in cmd_items:
        cmd_id = list_item.get("id")
        val = list_item.get("text")
        raw_cmd = cmd_lookup_fn(cmd_id) if callable(cmd_lookup_fn) else {}
        raw_code = raw_cmd.get("code") if isinstance(raw_cmd, dict) else None
        strategy = strategy_lookup_fn(raw_code) if callable(strategy_lookup_fn) else None
        kind = getattr(strategy, "kind", None) or (kind_lookup_fn(raw_code) if callable(kind_lookup_fn) else None)
        cmd_prefix = f"{base_name}#{prefix_base}/cmd:{cmd_id}"
        changed = False

        if isinstance(val, str):
            seg_id = cmd_prefix
            if mode == "extract":
                if segments is not None:
                    segments.append(Segment(
                        id=seg_id,
                        source=val,
                        context={"file": base_name, "code": raw_code, "kind": kind},
                    ))
            else:
                new_val = translations.get(seg_id, val)
                if new_val != val:
                    changed = True
                list_item["text"] = new_val
        elif isinstance(val, list):
            new_list = []
            for sub_idx, sub_val in enumerate(val):
                seg_id = f"{cmd_prefix}/{sub_idx}"
                if isinstance(sub_val, str):
                    if mode == "extract":
                        if segments is not None:
                            segments.append(Segment(
                                id=seg_id,
                                source=sub_val,
                                context={"file": base_name, "code": raw_code, "index": sub_idx, "kind": kind},
                            ))
                    else:
                        new_sub = translations.get(seg_id, sub_val)
                        if new_sub != sub_val:
                            changed = True
                        new_list.append(new_sub)
                else:
                    new_list.append(sub_val)
            if mode == "apply":
                list_item["text"] = new_list
        elif isinstance(val, dict):
            new_dict = {}
            for k, sub_val in val.items():
                seg_id = f"{cmd_prefix}/{k}"
                if isinstance(sub_val, str):
                    if mode == "extract":
                        if segments is not None:
                            segments.append(Segment(
                                id=seg_id,
                                source=sub_val,
                                context={"file": base_name, "code": raw_code, "key": k, "kind": kind},
                            ))
                    else:
                        new_sub = translations.get(seg_id, sub_val)
                        if new_sub != sub_val:
                            changed = True
                        new_dict[k] = new_sub
                else:
                    new_dict[k] = sub_val
            if mode == "apply":
                list_item["text"] = new_dict

        if mode == "apply" and changed:
            modified_items.append(list_item)

    return modified_items


def _resolve_item_by_id(collection: Any, target_id: Any) -> Optional[Dict[str, Any]]:
    if isinstance(collection, dict):
        if target_id in collection and isinstance(collection[target_id], dict):
            return collection[target_id]
        for val in collection.values():
            if isinstance(val, dict) and val.get("id") == target_id:
                return val
        return None
    if isinstance(collection, list):
        # Se os itens possuem chave 'id' explícita, procura primeiro pelo id
        has_explicit_id = any(isinstance(el, dict) and "id" in el for el in collection)
        if has_explicit_id:
            for el in collection:
                if isinstance(el, dict) and el.get("id") == target_id:
                    return el
        if isinstance(target_id, int) and 0 <= target_id < len(collection):
            candidate = collection[target_id]
            if isinstance(candidate, dict):
                return candidate
    return None


def walk_event_commands(
    data: Any,
    file_type: str = "Map",
) -> Iterator[Tuple[Any, Optional[Any], int, Dict[str, Any]]]:
    """Iterador compartilhado para estruturas events -> pages -> list e CommonEvents (events -> list)."""
    if not isinstance(data, (dict, list)):
        return

    if file_type == "Map":
        if not isinstance(data, dict):
            return
        events = data.get("events")
        if isinstance(events, list):
            event_iter = ((ev.get("id"), ev) for ev in events if isinstance(ev, dict))
        elif isinstance(events, dict):
            event_iter = ((ev.get("id", ev_id), ev) for ev_id, ev in events.items() if isinstance(ev, dict))
        else:
            return

        for event_id, event in event_iter:
            pages = event.get("pages")
            if not isinstance(pages, list):
                continue
            for page_idx, page in enumerate(pages):
                if not isinstance(page, dict):
                    continue
                page_id = page.get("id", page_idx)
                cmd_list = page.get("list")
                if not isinstance(cmd_list, list):
                    continue
                for cmd_idx, cmd in enumerate(cmd_list):
                    if isinstance(cmd, dict):
                        yield (event_id, page_id, cmd_idx, cmd)

    elif file_type == "CommonEvents":
        if not isinstance(data, list):
            return
        for event in data:
            if not event or not isinstance(event, dict):
                continue
            cmd_list = event.get("list")
            if not isinstance(cmd_list, list):
                continue
            for cmd_idx, cmd in enumerate(cmd_list):
                if isinstance(cmd, dict):
                    yield (event.get("id"), None, cmd_idx, cmd)

    elif file_type == "Troops":
        if not isinstance(data, list):
            return
        for troop in data:
            if not troop or not isinstance(troop, dict):
                continue
            pages = troop.get("pages")
            if not isinstance(pages, list):
                continue
            for page_idx, page in enumerate(pages):
                if not isinstance(page, dict):
                    continue
                page_id = page.get("id", page_idx)
                cmd_list = page.get("list")
                if not isinstance(cmd_list, list):
                    continue
                for cmd_idx, cmd in enumerate(cmd_list):
                    if isinstance(cmd, dict):
                        yield (troop.get("id"), page_id, cmd_idx, cmd)


def extract_commands_tree(
    data: Any,
    file_type: str,
    extract_cmd_fn: Callable[[Dict[str, Any], int], Optional[Dict[str, Any]]],
) -> List[Dict[str, Any]]:
    results: List[Dict[str, Any]] = []
    owners_map: Dict[Any, Dict[str, Any]] = {}

    for owner_id, page_id, cmd_idx, cmd in walk_event_commands(data, file_type):
        text_obj = extract_cmd_fn(cmd, cmd_idx)
        if not text_obj or not text_obj.get("text"):
            continue

        if file_type == "CommonEvents":
            if owner_id not in owners_map:
                owners_map[owner_id] = {"id": owner_id, "list": []}
                results.append(owners_map[owner_id])
            owners_map[owner_id]["list"].append(text_obj)
        else:
            if owner_id not in owners_map:
                owners_map[owner_id] = {"id": owner_id, "pages": []}
                results.append(owners_map[owner_id])
            page_obj = next((p for p in owners_map[owner_id]["pages"] if p["id"] == page_id), None)
            if not page_obj:
                page_obj = {"id": page_id, "list": []}
                owners_map[owner_id]["pages"].append(page_obj)
            page_obj["list"].append(text_obj)

    return results


def insert_commands_tree(
    data: Any,
    translated_data: List[Dict[str, Any]],
    file_type: str,
    insert_cmd_fn: Callable[[Dict[str, Any], Dict[str, Any]], None],
) -> None:
    if not isinstance(translated_data, list):
        return

    if file_type == "Map":
        if not isinstance(data, dict):
            return
        events = data.get("events")
        if not isinstance(events, (list, dict)):
            return
        for event_data in translated_data:
            raw_event = _resolve_item_by_id(events, event_data.get("id"))
            if not raw_event:
                continue
            pages = raw_event.get("pages", [])
            for page_data in event_data.get("pages", []):
                raw_page = _resolve_item_by_id(pages, page_data.get("id"))
                if not raw_page:
                    continue
                cmd_list = raw_page.get("list", [])
                for list_item in page_data.get("list", []):
                    cmd_id = list_item.get("id")
                    if isinstance(cmd_id, int) and 0 <= cmd_id < len(cmd_list):
                        insert_cmd_fn(cmd_list[cmd_id], list_item)

    elif file_type == "CommonEvents":
        if not isinstance(data, (list, dict)):
            return
        for event_data in translated_data:
            raw_event = _resolve_item_by_id(data, event_data.get("id"))
            if not raw_event:
                continue
            cmd_list = raw_event.get("list", [])
            for list_item in event_data.get("list", []):
                cmd_id = list_item.get("id")
                if isinstance(cmd_id, int) and 0 <= cmd_id < len(cmd_list):
                    insert_cmd_fn(cmd_list[cmd_id], list_item)

    elif file_type == "Troops":
        if not isinstance(data, (list, dict)):
            return
        for troop_data in translated_data:
            raw_troop = _resolve_item_by_id(data, troop_data.get("id"))
            if not raw_troop:
                continue
            pages = raw_troop.get("pages", [])
            for page_data in troop_data.get("pages", []):
                raw_page = _resolve_item_by_id(pages, page_data.get("id"))
                if not raw_page:
                    continue
                cmd_list = raw_page.get("list", [])
                for list_item in page_data.get("list", []):
                    cmd_id = list_item.get("id")
                    if isinstance(cmd_id, int) and 0 <= cmd_id < len(cmd_list):
                        insert_cmd_fn(cmd_list[cmd_id], list_item)
