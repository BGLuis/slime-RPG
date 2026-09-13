import logging
from abc import ABC, abstractmethod
import src.utils.TextsUtils as TextsUtils

class TranslationContext:
    """Objeto de Contexto que viaja através do Pipeline"""
    def __init__(self, file_name, data, text, translate_instance, file_path_str, extractor):
        self.file_name = file_name
        self.data = data
        self.text = text  # Estado atual do texto
        self.original_text = text
        self.translate_instance = translate_instance
        self.file_path_str = file_path_str
        self.extractor = extractor
        self.mask_map = {}
        self.patterns = []
        self.all_cached = False
        self.cached_positions = {}   # int index -> cached string
        self.pending_indices = []    # list of int indices in all_strings needing translation
        self.pending_originals = []  # list of raw unmasked strings needing translation
        self.masked_pending = []     # list of masked strings needing translation
        self.translated_pending = [] # list of translated (masked) strings
        self.unmasked_pending = []   # list of unmasked translated strings
        self.fixed_pending = []      # list of fixed unmasked translated strings


class PipelineStep(ABC):
    """Contrato base para um Middleware/Passo do Pipeline"""
    @abstractmethod
    def process(self, context: TranslationContext) -> TranslationContext:
        pass


class GetPatternsStep(PipelineStep):
    def process(self, context: TranslationContext) -> TranslationContext:
        if getattr(context, 'all_cached', False):
            return context
        if context.extractor:
            context.patterns = context.extractor._get_patterns(context.file_name, context.data)
        return context


class CacheLookupStep(PipelineStep):
    def process(self, context: TranslationContext) -> TranslationContext:
        if not context.translate_instance or not hasattr(context.translate_instance, 'cache'):
            return context

        all_strings = TextsUtils.dictToList(context.text)
        if not all_strings:
            context.all_cached = True
            return context

        context.cached_positions = {}
        context.pending_indices = []
        context.pending_originals = []

        cache = context.translate_instance.cache

        # Um único lookup em lote (TranslationMemory.lookup_many) em vez de 2
        # queries por string sob um lock global. Dublês de teste que usam um dict
        # puro caem no fallback item-a-item.
        if hasattr(cache, 'lookup_many'):
            translatable = [s for s in all_strings if isinstance(s, str) and s.strip()]
            hits = cache.lookup_many(translatable) if translatable else {}
            for i, s in enumerate(all_strings):
                if not isinstance(s, str) or not s.strip():
                    context.cached_positions[i] = s
                elif s in hits:
                    context.cached_positions[i] = hits[s]
                else:
                    context.pending_indices.append(i)
                    context.pending_originals.append(s)
        else:
            for i, s in enumerate(all_strings):
                if not isinstance(s, str) or not s.strip():
                    # Whitespace, vazio ou não-string não precisa de tradução
                    context.cached_positions[i] = s
                elif s in cache:
                    context.cached_positions[i] = cache[s]
                else:
                    context.pending_indices.append(i)
                    context.pending_originals.append(s)

        if not context.pending_indices:
            # 100% dos textos foram encontrados no cache!
            resolved_list = [context.cached_positions[i] for i in range(len(all_strings))]
            if isinstance(context.text, str):
                context.text = resolved_list[0] if resolved_list else context.text
            else:
                TextsUtils.interactive_item(context.text, resolved_list)
            context.all_cached = True
            if context.extractor:
                context.extractor.add_threads_status({
                    'file': context.file_path_str,
                    'status': 'process',
                    'msg': "100% recuperado do cache (0 requisições)"
                })

        return context


class MaskingStep(PipelineStep):
    def process(self, context: TranslationContext) -> TranslationContext:
        if getattr(context, 'all_cached', False):
            return context

        try:
            if context.pending_originals:
                context.masked_pending, context.mask_map = TextsUtils.mask_tokens_in_structure(
                    context.pending_originals, context.patterns, prefix="__XTOK_"
                )
            else:
                context.text, context.mask_map = TextsUtils.mask_tokens_in_structure(
                    context.text, context.patterns, prefix="__XTOK_"
                )
        except Exception as e:
            logging.error(f"Erro no MaskingStep: {e}")
            context.mask_map = {}
        return context


class TranslationStep(PipelineStep):
    def process(self, context: TranslationContext) -> TranslationContext:
        if getattr(context, 'all_cached', False):
            return context

        def progress_callback(current, total, eta_seconds=None, msg=None):
            if msg is None:
                msg = f"Translating batch {current}/{total}"
                if eta_seconds is not None:
                    mins, secs = divmod(int(eta_seconds), 60)
                    msg += f" - ETA: {mins}m {secs}s"
            
            if context.extractor:
                context.extractor.add_threads_status({
                    'file': context.file_path_str,
                    'status': 'process',
                    'current': current,
                    'total': total,
                    'msg': msg
                })

        if context.extractor:
            context.extractor.add_threads_status({
                'file': context.file_path_str, 
                'status': 'process', 
                'msg': "Processing file via Pipeline"
            })
        
        if context.translate_instance:
            if hasattr(context, 'masked_pending') and context.masked_pending:
                context.translated_pending = context.translate_instance.translate_batch(
                    context.masked_pending, progress_callback
                )
            else:
                context.text = context.translate_instance.translator(context.text, progress_callback)
        return context


class UnmaskingStep(PipelineStep):
    def process(self, context: TranslationContext) -> TranslationContext:
        if getattr(context, 'all_cached', False):
            return context

        # Não engolir exceção aqui: um placeholder que não foi desmascarado não pode
        # ir parar no arquivo final do jogo. Deixa propagar para process_file tentar
        # de novo (com retry/backoff) em vez de gravar "__XTOK_..." visível ao jogador.
        if context.mask_map:
            if hasattr(context, 'translated_pending') and context.translated_pending:
                context.unmasked_pending = TextsUtils.unmask_tokens_in_structure(
                    context.translated_pending, context.mask_map
                )
            else:
                context.text = TextsUtils.unmask_tokens_in_structure(context.text, context.mask_map)
        else:
            if hasattr(context, 'translated_pending') and context.translated_pending:
                context.unmasked_pending = context.translated_pending
        return context


class FixingStep(PipelineStep):
    def process(self, context: TranslationContext) -> TranslationContext:
        if getattr(context, 'all_cached', False):
            return context

        try:
            if hasattr(context, 'unmasked_pending') and context.unmasked_pending:
                fixed_pending = context.extractor.fix_text_translate(
                    context.unmasked_pending, context.pending_originals
                ) if context.extractor else context.unmasked_pending
                context.fixed_pending = fixed_pending if fixed_pending is not None else context.unmasked_pending
            else:
                fixed_text = context.extractor.fix_text_translate(
                    context.text, context.original_text
                ) if context.extractor else context.text
                context.text = fixed_text if fixed_text is not None else context.text
        except Exception as e:
            logging.error(f"Erro no FixingStep: {e}")
            if hasattr(context, 'unmasked_pending') and context.unmasked_pending:
                context.fixed_pending = context.unmasked_pending
        return context


class CacheStoreStep(PipelineStep):
    def process(self, context: TranslationContext) -> TranslationContext:
        if getattr(context, 'all_cached', False):
            return context

        if hasattr(context, 'fixed_pending') and context.fixed_pending:
            # 1. Salvar no cache apenas textos limpos e finalizados (texto original -> texto traduzido e corrigido)
            if context.translate_instance and hasattr(context.translate_instance, 'cache'):
                cache = context.translate_instance.cache
                lang_src = getattr(context.translate_instance, 'lang_source', None)
                lang_tgt = getattr(context.translate_instance, 'lang_target', None)
                pairs = [
                    (orig, fixed)
                    for orig, fixed in zip(context.pending_originals, context.fixed_pending)
                    if isinstance(orig, str) and orig.strip() and fixed is not None
                    and "__xtok_" not in orig.lower() and "__xtok_" not in str(fixed).lower()
                    and not (lang_src and lang_tgt and lang_src != lang_tgt and orig.strip() == fixed.strip())
                ]
                if hasattr(cache, 'store_many'):
                    cache.store_many(pairs)
                else:
                    for orig, fixed in pairs:
                        cache[orig] = fixed

            # 2. Reconstruir a lista completa de strings mesclando cached_positions e fixed_pending
            all_strings = TextsUtils.dictToList(context.original_text)
            final_list = [None] * len(all_strings)
            for i, val in context.cached_positions.items():
                final_list[i] = val
            for idx_in_pending, full_idx in enumerate(context.pending_indices):
                if idx_in_pending < len(context.fixed_pending):
                    final_list[full_idx] = context.fixed_pending[idx_in_pending]
                else:
                    final_list[full_idx] = context.pending_originals[idx_in_pending]

            if isinstance(context.text, str):
                context.text = final_list[0] if final_list else context.text
            else:
                TextsUtils.interactive_item(context.text, final_list)

        return context


class TranslationPipeline:
    """Gerenciador do Pipeline (Chain of Responsibility modificado)"""
    def __init__(self):
        self.steps = []

    def add_step(self, step: PipelineStep):
        self.steps.append(step)
        return self

    def execute(self, context: TranslationContext) -> str:
        for step in self.steps:
            context = step.process(context)
        return context.text
