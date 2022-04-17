package artransfer

import (
	"fmt"
	"os"

	"github.com/ArtalkJS/ArtalkGo/lib"
	"github.com/ArtalkJS/ArtalkGo/model"
	"github.com/cheggaaa/pb/v3"
)

var ArtransImporter = &_ArtransImporter{
	ImporterInfo: ImporterInfo{
		Name: "artrans",
		Desc: "从 Artrans 导入数据",
		Note: "",
	},
}

type _ArtransImporter struct {
	ImporterInfo
}

func (imp *_ArtransImporter) Run(basic *BasicParams, payload []string) {
	// 读取文件
	jsonStr, jErr := JsonFileReady(payload)
	if jErr != nil {
		logFatal(jErr)
		return
	}

	ImportArtransByStr(basic, jsonStr)
}

func ImportArtransByStr(basic *BasicParams, str string) {
	// 解析内容
	comments := []model.Artran{}
	dErr := JsonDecodeFAS(str, &comments)
	if dErr != nil {
		logFatal(dErr)
		return
	}

	ImportArtrans(basic, comments)
}

func ImportArtrans(basic *BasicParams, comments []model.Artran) {
	if len(comments) == 0 {
		logFatal("未读取到任何一条评论")
		return
	}

	if basic.TargetSiteUrl != "" && !lib.ValidateURL(basic.TargetSiteUrl) {
		logFatal("目标站点 URL 无效")
		return
	}

	// 汇总
	print("# 请过目：\n\n")

	// 第一条评论
	PrintEncodeData("第一条评论", comments[0])

	showTSiteName := basic.TargetSiteName
	showTSiteUrl := basic.TargetSiteUrl
	if showTSiteName == "" {
		showTSiteName = "未指定"

	}
	if showTSiteUrl == "" {
		showTSiteUrl = "未指定"
	}

	// 目标站点名和目标站点URL都不为空，才开启 URL 解析器
	showUrlResolver := "off"
	if basic.TargetSiteName != "" && basic.TargetSiteUrl != "" {
		basic.UrlResolver = true
		showUrlResolver = "on"
	}

	PrintTable([][]interface{}{
		{"目标站点名", showTSiteName},
		{"目标站点 URL", showTSiteUrl},
		{"评论数量", fmt.Sprintf("%d", len(comments))},
		{"URL 解析器", showUrlResolver},
	})

	print("\n")

	// 确认开始
	if !Confirm("确认开始导入吗？") {
		os.Exit(0)
	}

	// 准备导入评论
	print("\n")

	// 执行导入
	idMap := map[string]int{}    // ID 映射表 object_id => id
	idChanges := map[uint]uint{} // ID 变更表 original_id => new_db_id

	// 生成 ID 映射表
	id := 1
	for _, c := range comments {
		idMap[c.ID] = id
		id++
	}

	// 进度条
	var bar *pb.ProgressBar
	if HttpOutput == nil {
		bar = pb.StartNew(len(comments))
	}

	total := len(comments)

	// 遍历导入 comments
	for i, c := range comments {
		siteName := c.SiteName
		siteUrls := c.SiteUrls

		if basic.TargetSiteName != "" {
			siteName = basic.TargetSiteName
		}
		if basic.TargetSiteUrl != "" {
			siteUrls = basic.TargetSiteUrl
		}

		// 准备 site
		site, sErr := SiteReady(siteName, siteUrls)
		if sErr != nil {
			logFatal(sErr)
			return
		}

		// 准备 user
		user := model.FindCreateUser(c.Nick, c.Email, c.Link)
		if c.Password != "" {
			user.Password = c.Password
		}
		if c.BadgeName != "" {
			user.BadgeName = c.BadgeName
		}
		if c.BadgeColor != "" {
			user.BadgeColor = c.BadgeColor
		}
		model.UpdateUser(&user)

		// 准备 page
		nPageKey := c.PageKey
		if basic.UrlResolver { // 使用 URL 解析器
			nPageKey = UrlResolverGetPageKey(basic.TargetSiteUrl, c.PageKey)
		}

		page := model.FindCreatePage(nPageKey, c.PageTitle, site.Name)
		page.AdminOnly = c.PageAdminOnly == lib.ToString(true)
		model.UpdatePage(&page)

		// 创建新 comment 实例
		nComment := model.Comment{
			Rid: uint(idMap[c.Rid]),

			Content: c.Content,

			UA: c.UA,
			IP: c.IP,

			IsCollapsed: c.IsCollapsed == lib.ToString(true),
			IsPending:   c.IsPending == lib.ToString(true),
			IsPinned:    c.IsPending == lib.ToString(true),

			UserID:   user.ID,
			PageKey:  page.Key,
			SiteName: site.Name,
		}

		// 保存到数据库
		dErr := lib.DB.Create(&nComment).Error
		if dErr != nil {
			logError(fmt.Sprintf("评论源 ID:%s 保存失败", c.ID))
			continue
		}

		// 日期恢复
		// @see https://gorm.io/zh_CN/docs/conventions.html#CreatedAt
		lib.DB.Model(&nComment).Update("CreatedAt", ParseDate(c.CreatedAt))
		lib.DB.Model(&nComment).Update("UpdatedAt", ParseDate(c.UpdatedAt))

		idChanges[uint(idMap[c.ID])] = nComment.ID

		if bar != nil {
			bar.Increment()
		}
		if HttpOutput != nil && i%50 == 0 {
			print(fmt.Sprintf("%.0f%%... ", float64(i)/float64(total)*100))
		}
	}
	if bar != nil {
		bar.Finish()
	}
	if HttpOutput != nil {
		println()
	}
	logInfo(fmt.Sprintf("导入 %d 条数据", len(comments)))

	// reply id 重建
	RebuildRid(idChanges)
}
