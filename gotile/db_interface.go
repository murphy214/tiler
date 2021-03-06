package tile_surge

import (
	//"github.com/golang/protobuf/proto"
	"fmt"
	"github.com/jackc/pgx"
	_ "github.com/lib/pq"
	m "github.com/murphy214/mercantile"
	pc "github.com/murphy214/polyclip"
	"github.com/paulmach/go.geojson"
	"strconv"
	"strings"
	"sort"
	//"sync"
)

// returns the bbox logic from a table name and tileid
func Add_BBox(tablename string, tileid m.TileID) string {
	bds := m.Bounds(tileid)

	return fmt.Sprintf("(%s.geom && ST_MakeEnvelope(%f, %f, %f, %f, 4326))", tablename, bds.W, bds.S, bds.E, bds.N)

}

// this function allows you interface with a postgis database
// it create a raw feature collection geojson representation
// which would be the same if you were just reading from a geojson
func DB_Interface(database string, query string) *geojson.FeatureCollection {
	// intializing the config
	a := pgx.ConnPoolConfig{
		ConnConfig: pgx.ConnConfig{
			Host:     "localhost",
			Port:     5432,
			Database: database,
			User:     "postgres",
		},
		MaxConnections: 1,
	}

	// creating the connection
	p, _ := pgx.NewConnPool(a)

	// executing the query
	rows, _ := p.Query(query)

	// getting keys
	var keys []string
	fdescs := rows.FieldDescriptions()
	for _, i := range fdescs {
		keys = append(keys, i.Name)
	}

	pos := len(keys) - 1
	featcollection := &geojson.FeatureCollection{}

	// iterating through each row of the queried data
	for rows.Next() {
		// creating properties map
		var geometry geojson.Geometry

		vals, _ := rows.Values()
		tempmap := map[string]interface{}{}
		for ii, i := range vals[:pos] {
			tempmap[keys[ii]] = i
		}
		geometry.Scan(vals[pos])
		feature := geojson.Feature{Geometry: &geometry, Properties: tempmap}
		featcollection.Features = append(featcollection.Features, &feature)


	}

	return featcollection
}

// checking number of rows
func checkCount(rows *pgx.Rows) (count int) {
 	for rows.Next() {
    	rows.Scan(&count)
    }   
    return count
}


// getting the extent of a given database
func Get_Extent(database string,tablename string) (m.Extrema,int) {
	sqlquery := fmt.Sprintf("SELECT ST_Extent(geom) as table_extent FROM %s;",tablename)

	// intializing the config
	a := pgx.ConnPoolConfig{
		ConnConfig: pgx.ConnConfig{
			Host:     "localhost",
			Port:     5432,
			Database: database,
			User:     "postgres",
		},
		MaxConnections: 1,
	}

	// creating the connection
	p, _ := pgx.NewConnPool(a)

	rows, _ := p.Query(sqlquery)
	var bbox string
	for rows.Next() {
		vals, _ := rows.Values()
		bbox = vals[0].(string)
	}
	bbox = bbox[4:len(bbox) - 1]
	bbox = strings.Replace(bbox,","," ",1)
	vals := strings.Split(bbox," ")
	west,_ := strconv.ParseFloat(vals[0],64)
	south,_ := strconv.ParseFloat(vals[1],64)
	east,_ := strconv.ParseFloat(vals[2],64)
	north,_ := strconv.ParseFloat(vals[3],64)

    rows, _ = p.Query(fmt.Sprintf("SELECT COUNT(*) as count FROM  %s",tablename))
 	countrows := checkCount(rows)

	return m.Extrema{N:north,S:south,E:east,W:west},countrows
}


// evavulate extrema
func Lint_Extrema(ext m.Extrema,minzoom int) m.Extrema {
	// getting en
	en := []float64{ext.E,ext.N}
	en_tile := m.Tile(en[0],en[1],minzoom)
	en_bounds := m.Bounds(en_tile) 
	ext.N = en_bounds.N
	ext.E = en_bounds.E

	// getting ws
	ws := []float64{ext.W,ext.S}
	ws_tile := m.Tile(ws[0],ws[1],minzoom)
	ws_bounds := m.Bounds(ws_tile) 
	ext.S = ws_bounds.S
	ext.W = ws_bounds.W

	return ext
}

// making the tilemap for each tileslice in a given row
func Make_Tilelist(ext m.Extrema,minzoom int) []m.TileID {
	// getting linted extrema
	ext = Lint_Extrema(ext,minzoom)
	
	tileid := m.Tile(ext.W,ext.S,minzoom)
	bds := m.Bounds(tileid)
	startpt := []float64{(bds.E+bds.W)/2.0,(bds.N+bds.S)/2.0}
	currenty := startpt[1]
	currentx := startpt[0]
	startx := startpt[0]
	size := pc.Point{bds.E-bds.W,bds.N-bds.S}
	tilelist := []m.TileID{}


	for currenty < ext.N {
		currentx = startx
		for currentx < ext.E {
			tileid := m.Tile(currentx,currenty,minzoom)
			tilelist = append(tilelist,tileid)
			currentx += size.X
		}
		currenty += size.Y
	}
	return tilelist

}

// type for creating upper zoom data sets
type Extent struct {
	Bds m.Extrema
	Area float64
	Unique interface{}
}

// getting teh database extents
func DB_Extents(database string,tablename string,uniquefield string) []Extent {
	sqlquery := fmt.Sprintf("SELECT %s,ST_AsGeoJSON(ST_Envelope(geom)) FROM %s;",uniquefield,tablename)

	// intializing the config
	a := pgx.ConnPoolConfig{
		ConnConfig: pgx.ConnConfig{
			Host:     "localhost",
			Port:     5432,
			Database: database,
			User:     "postgres",
		},
		MaxConnections: 1,
	}

	// creating the connection
	p, _ := pgx.NewConnPool(a)

	rows, _ := p.Query(sqlquery)
	//var bbox string
	var id interface{} 
	var geom geojson.Geometry
	extents := []Extent{}
	for rows.Next() {
		vals,_ := rows.Values()
		geom.Scan(vals[1])

		id = vals[0]
		//bbox = vals[0].(string)
		val := geom.Polygon[0]
		bds := m.Extrema{S:val[0][1],N:val[1][1],W:val[0][0],E:val[2][0]}

		area := (bds.N - bds.S) * (bds.E - bds.W)
		extents = append(extents,Extent{Bds:bds,Area:area,Unique:id})

	}
	sort.Slice(extents, func(i, j int) bool { return extents[i].Area > extents[j].Area })

	return extents
}






