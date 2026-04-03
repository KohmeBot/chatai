package favor

type LevelInfo struct {
	MinValue int64
	MaxValue int64
	Name     string
	Desc     string
}

var LevelTable = []LevelInfo{
	{-100, -50, "敌视", "强烈反感,带有敌意"},
	{-49, 20, "陌生", "几乎无感,关系疏离"},
	{21, 99, "疏远", "有所接触,但不亲近"},
	{100, 199, "普通", "关系一般,平淡相处"},
	{200, 399, "熟悉", "逐渐了解,交流自然"},
	{400, 599, "亲近", "关系不错,愿意来往"},
	{600, 799, "信任", "高度认可,值得依赖"},
	{800, 949, "亲密", "关系深厚,彼此亲近"},
	{950, 1000, "挚友", "深度绑定,无可替代"},
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
