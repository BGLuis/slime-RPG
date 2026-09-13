from enum import IntEnum

class RPGEventCode(IntEnum):
    SHOW_TEXT = 401
    SHOW_TEXT_MORE = 405
    SCROLL_TEXT = 105
    COMMENT = 108
    COMMENT_MORE = 408
    LABEL = 118
    SHOW_CHOICES = 102
    SCRIPT = 355
    SCRIPT_MORE = 655
    SHOW_NAME = 101
    CHANGE_NAME = 320
    CHANGE_NICKNAME = 324
    CHANGE_PROFILE = 325
    CONTROL_VARIABLES = 122
    PLUGIN_COMMAND_MV = 356
    PLUGIN_COMMAND_MZ = 357
    CHOICE_BRANCH = 402
    SET_MOVEMENT_ROUTE = 205
    ROUTE_STEP = 505
    UNKNOWN = -1

    @classmethod
    def from_code(cls, code):
        try:
            return cls(code)
        except ValueError:
            return cls.UNKNOWN
