package bfocus

// Version é a versão desta SDK.
//
// Não é cosmética: vai no header X-Bfocus-Client ("bfocus-go/<Version>") de toda
// requisição — é por ele que a API sabe avisar quem roda uma versão afetada por uma
// correção. O scripts/release-sdks.sh do monorepo bumpa esta linha e o publish.yml do
// espelho recusa a tag vX.Y.Z que não bater com ela (em Go, a versão publicada é a tag).
const Version = "0.2.5"
