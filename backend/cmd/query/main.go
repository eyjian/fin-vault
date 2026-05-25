package main

import (
	"database/sql"
	"fmt"
	"log"

	_ "github.com/glebarez/go-sqlite"
)

func main() {
	db, err := sql.Open("sqlite", "data/finvault.db")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	// 查询资产
	fmt.Println("=== Assets (UserID=1) ===")
	rows, err := db.Query("SELECT f_id, f_user_id, f_asset_code, f_name, f_asset_type FROM t_fv_core_assets WHERE f_user_id = 1 AND f_deleted_at IS NULL")
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()

	for rows.Next() {
		var id, userID uint
		var code, name, assetType string
		if err := rows.Scan(&id, &userID, &code, &name, &assetType); err != nil {
			log.Fatal(err)
		}
		fmt.Printf("ID: %d, UserID: %d, Code: %s, Name: %s, Type: %s\n", id, userID, code, name, assetType)
	}

	// 查询持仓
	fmt.Println("\n=== Holdings (UserID=1) ===")
	rows, err = db.Query("SELECT f_id, f_user_id, f_asset_id, f_quantity, f_avg_cost, f_total_cost, f_realized_pnl, f_total_dividend FROM t_fv_core_holdings WHERE f_user_id = 1 AND f_deleted_at IS NULL")
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()

	for rows.Next() {
		var id, userID, assetID uint
		var quantity, avgCost, totalCost, realizedPnL, totalDividend string
		if err := rows.Scan(&id, &userID, &assetID, &quantity, &avgCost, &totalCost, &realizedPnL, &totalDividend); err != nil {
			log.Fatal(err)
		}
		fmt.Printf("ID: %d, UserID: %d, AssetID: %d, Qty: %s, AvgCost: %s, TotalCost: %s, RealizedPnL: %s, Dividend: %s\n",
			id, userID, assetID, quantity, avgCost, totalCost, realizedPnL, totalDividend)
	}

	// 查询最新行情
	fmt.Println("\n=== Latest Quotes ===")
	rows, err = db.Query("SELECT f_asset_id, f_price, f_time FROM t_fv_core_quotes ORDER BY f_time DESC LIMIT 10")
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()

	for rows.Next() {
		var assetID uint
		var price string
		var time string
		if err := rows.Scan(&assetID, &price, &time); err != nil {
			log.Fatal(err)
		}
		fmt.Printf("AssetID: %d, Price: %s, Time: %s\n", assetID, price, time)
	}
}
