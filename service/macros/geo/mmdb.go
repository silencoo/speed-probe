package geo

import (
	"github.com/silencoo/speed-probe/interfaces"
	"github.com/silencoo/speed-probe/utils"
)

func RunMMDBCheck(rawIp string) *interfaces.GeoInfo {
	if record := utils.QueryMaxMindDB(rawIp); record != nil {
		return &interfaces.GeoInfo{
			ASN:          record.ASN,
			ASNOrg:       record.ASNOrg,
			Org:          record.ASNOrg,
			Source:       "MaxMind (local)",
			LookupStatus: "available",
			IP:           rawIp,

			Country:       record.Country.Names.EN,
			CountryCode:   record.Country.ISOCode,
			City:          record.City.Names.EN,
			ContinentCode: record.Continent.Code,
			TimeZone:      record.Location.TimeZone,
			Lat:           record.Location.Latitude,
			Lon:           record.Location.Longitude,
		}
	}

	return nil
}
