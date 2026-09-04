from enum import IntEnum

class WolfEventCode(IntEnum):
    BLANK = 0
    CHECKPOINT = 99
    SHOW_MESSAGE = 101       # Caixa de texto principal / Diálogo
    SHOW_CHOICES = 102       # Escolhas / Opções para o jogador
    COMMENT = 103            # Comentários do desenvolvedor
    FORCE_STOP_MESSAGE = 105
    DEBUG_MESSAGE = 106      # Mensagem de depuração
    CLEAR_DEBUG_TEXT = 107
    VARIABLE_CONDITION = 111
    STRING_CONDITION = 112   # Condicional baseada em texto
    SET_VARIABLE = 121
    SET_STRING = 122         # Atribuição de variável de texto
    INPUT_KEY = 123
    SET_VARIABLE_EX = 124
    AUTO_INPUT = 125
    BAN_INPUT = 126
    TELEPORT = 130
    SOUND = 140
    PICTURE = 150            # Exibição de imagem ou texto renderizado como picture
    CHANGE_COLOR = 151
    SET_TRANSITION = 160
    PREPARE_TRANSITION = 161
    EXECUTE_TRANSITION = 162
    START_LOOP = 170
    BREAK_LOOP = 171
    BREAK_EVENT = 172
    ERASE_EVENT = 173
    RETURN_TO_TITLE = 174
    END_GAME = 175
    START_LOOP2 = 176
    MOVE = 201
    WAIT_FOR_MOVE = 202
    CALL_COMMON_EVENT = 210  # Chamada de evento comum
    CALL_COMMON_RESERVE = 211
    SET_LABEL = 212
    JUMP_LABEL = 213
    SAVE_LOAD = 220
    LOAD_GAME = 221
    SAVE_GAME = 222
    DATABASE = 250           # Operação em banco de dados
    IMPORT_DATABASE = 251
    PARTY = 270
    CALL_COMMON_BY_NAME = 300# Chamada de evento comum por nome
    CHOICE_CASE = 401        # Ramo de escolha
    SPECIAL_CHOICE_CASE = 402
    ELSE_CASE = 420
    UNKNOWN = -1

    @classmethod
    def from_code(cls, code):
        try:
            return cls(code)
        except ValueError:
            return cls.UNKNOWN
