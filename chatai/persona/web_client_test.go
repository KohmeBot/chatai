package persona

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"
)

func TestExtractWebMarkdownUsesReadableArticle(t *testing.T) {
	paragraph := strings.Repeat("这是一段用于验证正文抽取的中文内容，包含足够多的信息以便识别为文章。", 20)
	source := `<!doctype html>
<html lang="zh-CN">
<head>
  <title>测试文章 - 示例站点</title>
  <meta property="og:site_name" content="示例站点">
  <meta name="author" content="张三">
</head>
<body>
  <nav>不应出现在结果中的导航</nav>
  <article>
    <h1>测试文章</h1>
    <p>` + paragraph + `</p>
    <h2>章节标题</h2>
    <ul><li>第一项</li><li>第二项</li></ul>
    <p><a href="/reference">参考资料</a></p>
  </article>
  <aside>不应出现在结果中的推荐内容</aside>
</body>
</html>`

	result, err := extractWebMarkdown(source, "https://example.com/articles/1", "章节标题")
	require.NoError(t, err)
	require.Contains(t, result, "# 测试文章")
	require.Contains(t, result, "- 来源：<https://example.com/articles/1>")
	require.Contains(t, result, "- 网站：示例站点")
	require.Contains(t, result, "- 作者：张三")
	require.Contains(t, result, "## 正文")
	require.Contains(t, result, "## 章节标题")
	require.Contains(t, result, "- 第一项")
	require.Contains(t, result, "[参考资料](https://example.com/reference)")
	require.NotContains(t, result, "不应出现在结果中的导航")
	require.NotContains(t, result, "不应出现在结果中的推荐内容")
}

func TestFormatFallbackMarkdownIncludesPageTitleAndSource(t *testing.T) {
	result := formatFallbackMarkdown(
		`<html><head><title> 目录 &amp; 索引 </title></head></html>`,
		"https://example.com/directory",
		"条目一\n条目二",
	)

	require.Equal(t, "# 目录 & 索引\n\n- 来源：<https://example.com/directory>\n\n## 正文\n\n条目一\n条目二", result)
}

func TestExtractWebMarkdownFallsBackForNonArticlePage(t *testing.T) {
	source := `<html><head><title>工具目录</title></head><body><main><a href="/home">首页</a></main></body></html>`

	result, err := extractWebMarkdown(source, "https://example.com/directory", "首页")

	require.NoError(t, err)
	require.Contains(t, result, "# 工具目录")
	require.Contains(t, result, "[首页](https://example.com/home)")
}

func TestLimitWebContentReturnsAllContentBelowLimit(t *testing.T) {
	content := "# 页面\n\n- 来源：<https://example.com>\n\n## 正文\n\n完整内容与查询无关"

	result := limitWebContent(content, "不存在的查询", len(content)+1)

	require.Equal(t, content, result)
}

func TestLimitWebContentSelectsQueryContextAboveLimit(t *testing.T) {
	header := "# 长网页\n\n- 来源：<https://example.com>\n\n## 正文\n\n"
	body := strings.Repeat("开头无关内容。", 300) +
		"\n\n相关前文：任务正在等待发射。\n\n火星样本返回计划将在这里详细说明。\n\n相关后文：返回舱将在沙漠着陆。\n\n" +
		strings.Repeat("结尾无关内容。", 300)

	result := limitWebContent(header+body, "火星样本 返回计划", 1200)

	require.LessOrEqual(t, len(result), 1200)
	require.True(t, utf8.ValidString(result))
	require.Contains(t, result, "火星样本返回计划")
	require.Contains(t, result, "任务正在等待发射")
	require.Contains(t, result, "返回舱将在沙漠着陆")
	require.Contains(t, result, "已省略无关内容")
	require.NotContains(t, result, strings.Repeat("开头无关内容。", 20))
}

func TestLimitWebContentFallsBackToBeginningWithoutMatch(t *testing.T) {
	header := "# 长网页\n\n- 来源：<https://example.com>\n\n## 正文\n\n"
	body := "正文开头" + strings.Repeat("中文内容", 200)

	result := limitWebContent(header+body, "完全不存在", 300)

	require.LessOrEqual(t, len(result), 300)
	require.True(t, utf8.ValidString(result))
	require.Contains(t, result, "未找到与查询")
	require.Contains(t, result, "正文开头")
}

func TestBrowseWebRequiresQuery(t *testing.T) {
	_, err := (&Persona{}).handleBrowseWeb(nil, []byte(`{"url":"https://example.com"}`))

	require.ErrorContains(t, err, "query must not be empty")
}
