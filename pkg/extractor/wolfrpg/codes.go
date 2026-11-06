package wolfrpg

// WolfEventCode representa os códigos de comandos de eventos da engine WOLF RPG.
type WolfEventCode int

const (
	CodeBlank             WolfEventCode = 0
	CodeCheckpoint        WolfEventCode = 99
	CodeShowMessage       WolfEventCode = 101 // Diálogos e mensagens na tela
	CodeShowChoices       WolfEventCode = 102 // Escolhas e opções
	CodeComment           WolfEventCode = 103 // Comentários internos
	CodeForceStopMessage  WolfEventCode = 105
	CodeDebugMessage      WolfEventCode = 106
	CodeClearDebugText    WolfEventCode = 107
	CodeVariableCondition WolfEventCode = 111
	CodeStringCondition   WolfEventCode = 112 // Condicional por texto
	CodeSetVariable       WolfEventCode = 121
	CodeSetString         WolfEventCode = 122 // Atribuição de string/variável
	CodeInputKey          WolfEventCode = 123
	CodeSetVariableEx     WolfEventCode = 124
	CodeAutoInput         WolfEventCode = 125
	CodeBanInput          WolfEventCode = 126
	CodeTeleport          WolfEventCode = 130
	CodeSound             WolfEventCode = 140
	CodePicture           WolfEventCode = 150 // Exibição de imagem ou texto renderizado
	CodeChangeColor       WolfEventCode = 151
	CodeSetTransition     WolfEventCode = 160
	CodePrepareTransition WolfEventCode = 161
	CodeExecuteTransition WolfEventCode = 162
	CodeStartLoop         WolfEventCode = 170
	CodeBreakLoop         WolfEventCode = 171
	CodeBreakEvent        WolfEventCode = 172
	CodeEraseEvent        WolfEventCode = 173
	CodeReturnToTitle     WolfEventCode = 174
	CodeEndGame           WolfEventCode = 175
	CodeStartLoop2        WolfEventCode = 176
	CodeMove              WolfEventCode = 201
	CodeWaitForMove       WolfEventCode = 202
	CodeCallCommonEvent   WolfEventCode = 210
	CodeCallCommonReserve WolfEventCode = 211
	CodeSetLabel          WolfEventCode = 212
	CodeJumpLabel         WolfEventCode = 213
	CodeSaveLoad          WolfEventCode = 220
	CodeLoadGame          WolfEventCode = 221
	CodeSaveGame          WolfEventCode = 222
	CodeDatabase          WolfEventCode = 250
	CodeImportDatabase    WolfEventCode = 251
	CodeParty             WolfEventCode = 270
	CodeCallCommonByName  WolfEventCode = 300
	CodeChoiceCase        WolfEventCode = 401
	CodeSpecialChoiceCase WolfEventCode = 402
	CodeElseCase          WolfEventCode = 420
	CodeUnknown           WolfEventCode = -1
)
