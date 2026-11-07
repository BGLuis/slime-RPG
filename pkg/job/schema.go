package job

import (
	"strings"

	"github.com/BGLuis/slime-rpg/pkg/formats"
)

// ExtractorSchema descreve as opções do extrator para as interfaces desenharem o
// formulário. name aceita o nome do extrator (-e) ou o da engine detectada; sem
// opções configuráveis devolve um schema vazio. As chaves são as de
// Config.ExtractorOptions.
func ExtractorSchema(name string) ConfigSchema {
	canonical, _ := formats.ExtensionsFor(name)
	switch canonical {
	case "csv":
		return ConfigSchema{Fields: []ConfigSchemaField{
			{Key: formats.OptCSVColumns, Label: "Colunas a traduzir", Type: "text",
				Help: "Nomes separados por vírgula (ex: value,text). Vazio = detecção automática."},
			{Key: formats.OptCSVDelimiter, Label: "Delimitador", Type: "text",
				Help: `Ex: , ; | ou \t. Vazio = detecção automática.`},
		}}
	case "json":
		return ConfigSchema{Fields: []ConfigSchemaField{
			{Key: formats.OptJSONKeys, Label: "Chaves a traduzir", Type: "text",
				Help: "Nomes separados por vírgula (ex: text,name,description) ou * para todas. Vazio = chaves padrão."},
		}}
	case "wolfrpg":
		return ConfigSchema{Fields: []ConfigSchemaField{
			{Key: formats.OptWolfPreserveTit, Label: "Manter título original", Type: "bool", Default: "true",
				Help: "true mantém o título do Game.dat, o que evita invalidar saves existentes."},
			{Key: formats.OptWolfTargetFont, Label: "Fonte", Type: "text",
				Help: "Nome da fonte a gravar no Game.dat. Vazio = mantém a fonte do jogo."},
			{Key: formats.OptWolfRepackMode, Label: "Reempacotar Data.wolf", Type: "bool", Default: "false",
				Help: "false grava os arquivos traduzidos soltos em Data/; true também gera Data/Data.wolf."},
		}}
	}
	return ConfigSchema{}
}

// ProviderSchema descreve as credenciais que o provedor pede na interface.
func ProviderSchema(provider string) ConfigSchema {
	if strings.EqualFold(provider, "deepl") {
		return ConfigSchema{Fields: []ConfigSchemaField{
			{Key: "api_key", Label: "Chave da API DeepL", Type: "secret", Required: true,
				Help: "Se vazio, usa a variável de ambiente DEEPL_API_KEY."},
		}}
	}
	return ConfigSchema{}
}
