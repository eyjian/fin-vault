package tools

// 此文件曾经放置 parseAssetType / parseHoldingStatus / parseTxnType 等枚举转换函数，
// 在 ListOptions.Filters 改造后这些类型转换交由 repository GORM 实现负责，
// tools 层全部以字符串/原始值传递，故本文件保留为空占位，后续如需 Tool 层共享辅助再添加。
