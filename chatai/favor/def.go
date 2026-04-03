package favor

type LevelInfo struct {
	MinValue int64
	MaxValue int64
	Name     string
	Desc     string
}

var LevelTable = []LevelInfo{
	{-100, -1, "敌视", "极度反感"},
	{0, 0, "陌生", "完全没有印象"},
	{1, 99, "疏远", "认识但不熟，态度冷淡"},
	{100, 199, "普通", "普通关系，无明显情绪"},
	{200, 399, "熟悉", "有一定了解，互动自然"},
	{400, 599, "亲近", "关系不错，愿意帮助"},
	{600, 799, "信任", "高信任度"},
	{800, 949, "亲密", "非常亲近，关系稳定"},
	{950, 1000, "挚友", "最高等级，强绑定关系"},
}

func GetFavorLevelInfo(favor int64) LevelInfo {
	if favor > FavorMax {
		favor = FavorMax
	}
	if favor < FavorMin {
		favor = FavorMin
	}

	for _, level := range LevelTable {
		if favor >= level.MinValue && favor <= level.MaxValue {
			return level
		}
	}

	// 理论不会走到这里
	return LevelInfo{
		Name: "未知",
		Desc: "未定义档位",
	}
}
