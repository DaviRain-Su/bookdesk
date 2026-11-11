package main

// 前端与 Go 之间传递的类型。JSON 字段名沿用前端已有的命名，
// 这样 src/mygo.ts 重新生成后前端不用改调用。

type Book struct {
	ID           string  `json:"id"`
	Title        string  `json:"title"`
	Author       *string `json:"author"`
	Language     *string `json:"language"`
	CoverRelPath *string `json:"coverRelPath"`
	EpubRelPath  string  `json:"epubRelPath"`
	Collection   *string `json:"collection"`
	ImportedAt   int64   `json:"importedAt"`
}

type ImportError struct {
	Path    string `json:"path"`
	Message string `json:"message"`
}

type ImportResult struct {
	Books  []Book        `json:"books"`
	Errors []ImportError `json:"errors"`
}

// ImportFolderResult 是一次文件夹导入的结果。Scanned 是找到的 EPUB 数，
// Skipped 是书名、作者和文件大小都和书架上的书相同的数量。
type ImportFolderResult struct {
	Books     []Book        `json:"books"`
	Errors    []ImportError `json:"errors"`
	Scanned   int64         `json:"scanned"`
	Skipped   int64         `json:"skipped"`
	Cancelled bool          `json:"cancelled"`
}

type ImportProgress struct {
	Done  int64  `json:"done"`
	Total int64  `json:"total"`
	Title string `json:"title"`
}

type BookLocation struct {
	BookID     string  `json:"bookId"`
	CFI        *string `json:"cfi"`
	SpineIndex int64   `json:"spineIndex"`
	Href       *string `json:"href"`
	PageLabel  *string `json:"pageLabel"`
	CharOffset *int64  `json:"charOffset"`
}

type Highlight struct {
	ID        string       `json:"id"`
	BookID    string       `json:"bookId"`
	Location  BookLocation `json:"location"`
	Text      string       `json:"text"`
	Color     string       `json:"color"`
	CreatedAt int64        `json:"createdAt"`
}

type Card struct {
	ID            string        `json:"id"`
	Type          string        `json:"type"`
	Title         *string       `json:"title"`
	Body          *string       `json:"body"`
	Quote         *string       `json:"quote"`
	QuoteLocation *BookLocation `json:"quoteLocation"`
	ImageRelPath  *string       `json:"imageRelPath"`
	SourceBookID  *string       `json:"sourceBookId"`
	CharacterName *string       `json:"characterName"`
	CreatedAt     int64         `json:"createdAt"`
	UpdatedAt     int64         `json:"updatedAt"`
}

// BoardData 是一块画布的完整状态；画布服务叫 Board，两个名字不能撞。
type BoardData struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	ViewportJSON string `json:"viewportJson"`
	NodesJSON    string `json:"nodesJson"`
	EdgesJSON    string `json:"edgesJson"`
	UpdatedAt    int64  `json:"updatedAt"`
}

type BoardSummary struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	Folder    *string `json:"folder"`
	UpdatedAt int64   `json:"updatedAt"`
}

type SearchHit struct {
	BookID     string       `json:"bookId"`
	Title      string       `json:"title"`
	Location   BookLocation `json:"location"`
	Snippet    string       `json:"snippet"`
	Highlights [][2]int64   `json:"highlights"`
}

type ChapterText struct {
	SpineIndex int64   `json:"spineIndex"`
	Href       *string `json:"href"`
	Text       string  `json:"text"`
}

type SaveCardInput struct {
	ID            *string       `json:"id"`
	Type          string        `json:"type"`
	Title         *string       `json:"title"`
	Body          *string       `json:"body"`
	Quote         *string       `json:"quote"`
	QuoteLocation *BookLocation `json:"quoteLocation"`
	ImageRelPath  *string       `json:"imageRelPath"`
	SourceBookID  *string       `json:"sourceBookId"`
	CharacterName *string       `json:"characterName"`
}

type SaveHighlightInput struct {
	ID       *string      `json:"id"`
	BookID   string       `json:"bookId"`
	Location BookLocation `json:"location"`
	Text     string       `json:"text"`
	Color    *string      `json:"color"`
}

// ---------- 人物与 AI ----------

type Character struct {
	ID           string  `json:"id"`
	BookID       string  `json:"bookId"`
	Name         string  `json:"name"`
	Description  *string `json:"description"`
	ImageRelPath *string `json:"imageRelPath"`
	CreatedAt    int64   `json:"createdAt"`
}

type SaveCharacterInput struct {
	ID           *string `json:"id"`
	BookID       string  `json:"bookId"`
	Name         string  `json:"name"`
	Description  *string `json:"description"`
	ImageRelPath *string `json:"imageRelPath"`
}

type AiStatus struct {
	HasKey       bool   `json:"hasKey"`
	Model        string `json:"model"`
	BaseURL      string `json:"baseUrl"`
	Engine       string `json:"engine"`
	AgentCommand string `json:"agentCommand"`
	AgentRunning bool   `json:"agentRunning"`
	AgentName    string `json:"agentName"`
}

// AcpUpdate 是外部 Agent 的流式输出，按 turnId 对应到前端那一次提问。
type AcpUpdate struct {
	TurnID string `json:"turnId"`
	Kind   string `json:"kind"`
	Text   string `json:"text"`
}

type AgentTest struct {
	OK      bool   `json:"ok"`
	Name    string `json:"name"`
	Message string `json:"message"`
}

// AgentCandidate 是设置页里可以点选的已知 ACP Agent。
type AgentCandidate struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Command string `json:"command"`
	State   string `json:"state"`
	Detail  string `json:"detail"`
}

type ChatInput struct {
	BookID               string  `json:"bookId"`
	BookTitle            string  `json:"bookTitle"`
	CharacterName        string  `json:"characterName"`
	CharacterDescription *string `json:"characterDescription"`
	Question             string  `json:"question"`
	VisibleText          string  `json:"visibleText"`
	ChapterTitle         *string `json:"chapterTitle"`
	CurrentSpineIndex    int64   `json:"currentSpineIndex"`
	AllowFuture          bool    `json:"allowFuture"`
	TurnID               string  `json:"turnId"`
	SessionKey           string  `json:"sessionKey"`
}

type ChatReply struct {
	OK      bool   `json:"ok"`
	Text    string `json:"text"`
	Message string `json:"message"`
}

// SelectionAskInput 是划选一段文字后「问 AI / 解释 / 翻译」的请求。
// 和人物对话一样，只带当前可见页和已读范围内的片段，不把整本书发出去。
type SelectionAskInput struct {
	BookID            string  `json:"bookId"`
	BookTitle         string  `json:"bookTitle"`
	Mode              string  `json:"mode"`
	Selection         string  `json:"selection"`
	Question          string  `json:"question"`
	VisibleText       string  `json:"visibleText"`
	ChapterTitle      *string `json:"chapterTitle"`
	CurrentSpineIndex int64   `json:"currentSpineIndex"`
	AllowFuture       bool    `json:"allowFuture"`
	TurnID            string  `json:"turnId"`
	SessionKey        string  `json:"sessionKey"`
}

type WordLookupInput struct {
	Word      string  `json:"word"`
	Sentence  *string `json:"sentence"`
	BookTitle *string `json:"bookTitle"`
	TurnID    string  `json:"turnId"`
}

// OpenFileEvent 把 Finder 双击打开的文件交给前端导入。
type OpenFileEvent struct {
	Path string `json:"path"`
}

// ImportedNote 是从用户自己的笔记文件里读出来的一条划线或想法。
// 只读本地文件，不联网；匹配到书库里的书时带上位置，方便以后跳回原文。
type ImportedNote struct {
	BookID    *string       `json:"bookId"`
	BookTitle string        `json:"bookTitle"`
	Author    *string       `json:"author"`
	Chapter   *string       `json:"chapter"`
	Quote     string        `json:"quote"`
	Note      *string       `json:"note"`
	Location  *BookLocation `json:"location"`
}

type ImportNotesResult struct {
	Notes   []ImportedNote `json:"notes"`
	Total   int64          `json:"total"`
	Skipped int64          `json:"skipped"`
	Errors  []ImportError  `json:"errors"`
}

// ShareCardInput 是一张划线分享卡要画的内容。
// IncludeNote 为 false 时，即使 Note 里有文字也不会画上去。
type ShareCardInput struct {
	Quote       string `json:"quote"`
	Note        string `json:"note"`
	IncludeNote bool   `json:"includeNote"`
	Title       string `json:"title"`
	Author      string `json:"author"`
	Chapter     string `json:"chapter"`
	Color       string `json:"color"`
	Template    string `json:"template"`
	Ratio       string `json:"ratio"`
}

// ShareCardImage 是渲染好的 PNG。只在本机生成，不上传。
type ShareCardImage struct {
	PNGBase64 string `json:"pngBase64"`
	Width     int64  `json:"width"`
	Height    int64  `json:"height"`
}

// ShareFileResult 是用户保存文件的结果。取消对话框时 Cancelled 为 true，Path 为空。
type ShareFileResult struct {
	Path      string `json:"path"`
	Cancelled bool   `json:"cancelled"`
}

// BookShareItem 是一本书可以放进公开页的一条划线。
// Note 是读者自己的笔记，只在本机的选择界面里出现；导出时默认不带上。
type BookShareItem struct {
	ID        string `json:"id"`
	Quote     string `json:"quote"`
	Chapter   string `json:"chapter"`
	Color     string `json:"color"`
	Note      string `json:"note"`
	CreatedAt int64  `json:"createdAt"`
}

// BookShareList 是导出前给用户看的一本书：元数据，加上可勾选的划线。
type BookShareList struct {
	BookID   string          `json:"bookId"`
	Title    string          `json:"title"`
	Author   string          `json:"author"`
	Language string          `json:"language"`
	Items    []BookShareItem `json:"items"`
}

// BookExportChoice 是用户勾选的一条。IncludeNote 默认应为 false。
type BookExportChoice struct {
	ID          string `json:"id"`
	IncludeNote bool   `json:"includeNote"`
}

// BookExportInput 是一次导出。Items 为空时只导出书名和作者，不带任何划线或笔记。
type BookExportInput struct {
	BookID string             `json:"bookId"`
	Items  []BookExportChoice `json:"items"`
}
