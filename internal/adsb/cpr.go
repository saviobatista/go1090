package adsb

import (
	"math"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// CPRDecoder handles CPR position decoding
type CPRDecoder struct {
	aircraftPositions map[uint32]*AircraftPosition
	positionMutex     sync.RWMutex
	logger            *logrus.Logger
	verbose           bool
}

// NewCPRDecoder creates a new CPR decoder
func NewCPRDecoder(logger *logrus.Logger, verbose bool) *CPRDecoder {
	return &CPRDecoder{
		aircraftPositions: make(map[uint32]*AircraftPosition),
		logger:            logger,
		verbose:           verbose,
	}
}

// DecodeCPRPosition decodes CPR coordinates to actual lat/lon using proper CPR algorithm
func (c *CPRDecoder) DecodeCPRPosition(icao uint32, fFlag uint8, latCPR, lonCPR uint32) (float64, float64) {
	now := time.Now()

	// Get or create aircraft position tracking
	c.positionMutex.Lock()
	aircraft, exists := c.aircraftPositions[icao]
	if !exists {
		aircraft = &AircraftPosition{
			ICAO:       icao,
			LastUpdate: now,
		}
		c.aircraftPositions[icao] = aircraft
	}
	c.positionMutex.Unlock()

	// Store the new frame
	newFrame := &CPRFrame{
		LatCPR:    latCPR,
		LonCPR:    lonCPR,
		FFlag:     fFlag,
		Timestamp: now,
	}

	if fFlag == 0 {
		aircraft.EvenFrame = newFrame
	} else {
		aircraft.OddFrame = newFrame
	}

	// Try to decode using both frames if available
	if aircraft.EvenFrame != nil && aircraft.OddFrame != nil {
		// Both frames available - use proper CPR decoding
		lat, lon := c.decodeCPRBothFrames(aircraft.EvenFrame, aircraft.OddFrame)
		if lat != 0 || lon != 0 {
			aircraft.LastPos = &Position{
				Latitude:  lat,
				Longitude: lon,
				Timestamp: now,
			}
			aircraft.LastUpdate = now

			if c.verbose {
				c.logger.Debugf("CPR decode: ICAO=%06X, both frames, lat=%.6f, lon=%.6f", icao, lat, lon)
			}
			return lat, lon
		}
	}

	// Single frame decoding (less accurate)
	lat, lon := c.decodeCPRSingleFrame(newFrame)
	if lat != 0 || lon != 0 {
		aircraft.LastPos = &Position{
			Latitude:  lat,
			Longitude: lon,
			Timestamp: now,
		}
		aircraft.LastUpdate = now

		if c.verbose {
			c.logger.Debugf("CPR decode: ICAO=%06X, single frame, lat=%.6f, lon=%.6f", icao, lat, lon)
		}
		return lat, lon
	}

	// Use last known position if available and recent
	if aircraft.LastPos != nil && now.Sub(aircraft.LastPos.Timestamp) < 30*time.Second {
		if c.verbose {
			c.logger.Debugf("CPR decode: ICAO=%06X, using last position, lat=%.6f, lon=%.6f", icao, aircraft.LastPos.Latitude, aircraft.LastPos.Longitude)
		}
		return aircraft.LastPos.Latitude, aircraft.LastPos.Longitude
	}

	return 0, 0
}

// cprModInt performs always positive MOD operation (dump1090 style)
func cprModInt(a, b int) int {
	res := a % b
	if res < 0 {
		res += b
	}
	return res
}

// decodeCPRBothFrames decodes position using both even and odd frames (dump1090 algorithm)
func (c *CPRDecoder) decodeCPRBothFrames(evenFrame, oddFrame *CPRFrame) (float64, float64) {
	// Use dump1090's exact CPR algorithm
	const CPR_MAX = 131072.0 // 2^17

	AirDlat0 := 360.0 / 60.0 // 6.0 degrees for even frame
	AirDlat1 := 360.0 / 59.0 // ~6.101 degrees for odd frame

	lat0 := float64(evenFrame.LatCPR)
	lat1 := float64(oddFrame.LatCPR)
	lon0 := float64(evenFrame.LonCPR)
	lon1 := float64(oddFrame.LonCPR)

	// Compute the Latitude Index "j" (dump1090 method)
	j := int(math.Floor(((59*lat0 - 60*lat1) / CPR_MAX) + 0.5))

	rlat0 := AirDlat0 * (float64(cprModInt(j, 60)) + lat0/CPR_MAX)
	rlat1 := AirDlat1 * (float64(cprModInt(j, 59)) + lat1/CPR_MAX)

	// Normalize latitudes (dump1090 method)
	if rlat0 >= 270 {
		rlat0 -= 360
	}
	if rlat1 >= 270 {
		rlat1 -= 360
	}

	// Check to see that the latitude is in range: -90 .. +90
	if rlat0 < -90 || rlat0 > 90 || rlat1 < -90 || rlat1 > 90 {
		if c.verbose {
			c.logger.Debugf("CPR: bad latitude data, rlat0=%.6f, rlat1=%.6f", rlat0, rlat1)
		}
		return 0, 0 // bad data
	}

	// Check that both are in the same latitude zone, or abort
	if c.cprNLTable(rlat0) != c.cprNLTable(rlat1) {
		if c.verbose {
			c.logger.Debugf("CPR: positions crossed latitude zone, nl0=%d, nl1=%d", c.cprNLTable(rlat0), c.cprNLTable(rlat1))
		}
		return 0, 0 // positions crossed a latitude zone, try again later
	}

	// Determine which frame to use (use most recent)
	var rlat, rlon float64

	if oddFrame.Timestamp.After(evenFrame.Timestamp) {
		// Use odd packet
		ni := c.cprNFunction(rlat1, 1)
		m := int(math.Floor((((lon0 * float64(c.cprNLTable(rlat1)-1)) -
			(lon1 * float64(c.cprNLTable(rlat1)))) / CPR_MAX) + 0.5))
		rlon = c.cprDlonFunction(rlat1, 1) * (float64(cprModInt(m, ni)) + lon1/CPR_MAX)
		rlat = rlat1
	} else {
		// Use even packet
		ni := c.cprNFunction(rlat0, 0)
		m := int(math.Floor((((lon0 * float64(c.cprNLTable(rlat0)-1)) -
			(lon1 * float64(c.cprNLTable(rlat0)))) / CPR_MAX) + 0.5))
		rlon = c.cprDlonFunction(rlat0, 0) * (float64(cprModInt(m, ni)) + lon0/CPR_MAX)
		rlat = rlat0
	}

	// Renormalize longitude to -180 .. +180 (dump1090 method)
	rlon -= math.Floor((rlon+180)/360) * 360

	if c.verbose {
		c.logger.Debugf("Both frames CPR: lat=%.6f, lon=%.6f, j=%d", rlat, rlon, j)
	}

	return rlat, rlon
}

// cprNFunction returns the number of longitude zones (dump1090 style)
func (c *CPRDecoder) cprNFunction(lat float64, fflag int) int {
	nl := c.cprNLTable(lat) - fflag
	if nl < 1 {
		nl = 1
	}
	return nl
}

// cprDlonFunction returns longitude zone width (dump1090 style)
func (c *CPRDecoder) cprDlonFunction(lat float64, fflag int) float64 {
	return 360.0 / float64(c.cprNFunction(lat, fflag))
}

// decodeCPRSingleFrame decodes position using a single frame (less accurate, requires reference position)
func (c *CPRDecoder) decodeCPRSingleFrame(frame *CPRFrame) (float64, float64) {
	// For single frame decoding, we need a reference position
	// Use a reasonable default for Brazil region: São Paulo area
	refLat := -23.5505 // São Paulo latitude
	refLon := -46.6333 // São Paulo longitude

	// Try to use a more recent known position if available
	c.positionMutex.Lock()
	for _, aircraft := range c.aircraftPositions {
		if aircraft.LastPos != nil && time.Since(aircraft.LastPos.Timestamp) < 5*time.Minute {
			refLat = aircraft.LastPos.Latitude
			refLon = aircraft.LastPos.Longitude
			break
		}
	}
	c.positionMutex.Unlock()

	const CPR_MAX = 131072.0 // 2^17

	// Use dump1090's single-frame algorithm with reference position
	lat := float64(frame.LatCPR)
	lon := float64(frame.LonCPR)

	// Calculate latitude zones
	AirDlat := 360.0 / 60.0
	if frame.FFlag == 1 {
		AirDlat = 360.0 / 59.0
	}

	// Calculate longitude zones
	j := int(math.Floor(refLat/AirDlat + 0.5))
	rlat := AirDlat * (float64(j) + lat/CPR_MAX)

	// Check if we need to adjust the latitude zone
	if (rlat - refLat) > (AirDlat / 2.0) {
		rlat -= AirDlat
	} else if (rlat - refLat) < -(AirDlat / 2.0) {
		rlat += AirDlat
	}

	// Calculate longitude
	ni := c.cprNFunction(rlat, int(frame.FFlag))
	if ni <= 0 {
		ni = 1
	}

	dlon := 360.0 / float64(ni)
	m := int(math.Floor(refLon/dlon + 0.5))
	rlon := dlon * (float64(m) + lon/CPR_MAX)

	// Check if we need to adjust the longitude zone
	if (rlon - refLon) > (dlon / 2.0) {
		rlon -= dlon
	} else if (rlon - refLon) < -(dlon / 2.0) {
		rlon += dlon
	}

	// Normalize longitude to -180 .. +180
	rlon -= math.Floor((rlon+180)/360) * 360

	// Validate the result
	if rlat < -90 || rlat > 90 {
		if c.verbose {
			c.logger.Debugf("Single frame CPR: invalid latitude %.6f", rlat)
		}
		return 0, 0
	}

	if c.verbose {
		c.logger.Debugf("Single frame CPR: lat=%.6f, lon=%.6f (ref: %.6f, %.6f)", rlat, rlon, refLat, refLon)
	}

	return rlat, rlon
}

// cprNLTable returns the number of longitude zones for a given latitude using lookup table
func (c *CPRDecoder) cprNLTable(lat float64) int {
	// NL lookup table based on latitude (more accurate than calculation)
	absLat := math.Abs(lat)

	if absLat < 10.47047130 {
		return 59
	}
	if absLat < 14.82817437 {
		return 58
	}
	if absLat < 18.18626357 {
		return 57
	}
	if absLat < 21.02939493 {
		return 56
	}
	if absLat < 23.54504487 {
		return 55
	}
	if absLat < 25.82924707 {
		return 54
	}
	if absLat < 27.93898710 {
		return 53
	}
	if absLat < 29.91135686 {
		return 52
	}
	if absLat < 31.77209708 {
		return 51
	}
	if absLat < 33.53993436 {
		return 50
	}
	if absLat < 35.22899598 {
		return 49
	}
	if absLat < 36.85025108 {
		return 48
	}
	if absLat < 38.41241892 {
		return 47
	}
	if absLat < 39.92256684 {
		return 46
	}
	if absLat < 41.38651832 {
		return 45
	}
	if absLat < 42.80914012 {
		return 44
	}
	if absLat < 44.19454951 {
		return 43
	}
	if absLat < 45.54626723 {
		return 42
	}
	if absLat < 46.86733252 {
		return 41
	}
	if absLat < 48.16039128 {
		return 40
	}
	if absLat < 49.42776439 {
		return 39
	}
	if absLat < 50.67150166 {
		return 38
	}
	if absLat < 51.89342469 {
		return 37
	}
	if absLat < 53.09516153 {
		return 36
	}
	if absLat < 54.27817472 {
		return 35
	}
	if absLat < 55.44378444 {
		return 34
	}
	if absLat < 56.59318756 {
		return 33
	}
	if absLat < 57.72747354 {
		return 32
	}
	if absLat < 58.84763776 {
		return 31
	}
	if absLat < 59.95459277 {
		return 30
	}
	if absLat < 61.04917774 {
		return 29
	}
	if absLat < 62.13216659 {
		return 28
	}
	if absLat < 63.20427479 {
		return 27
	}
	if absLat < 64.26616523 {
		return 26
	}
	if absLat < 65.31845310 {
		return 25
	}
	if absLat < 66.36171008 {
		return 24
	}
	if absLat < 67.39646774 {
		return 23
	}
	if absLat < 68.42322022 {
		return 22
	}
	if absLat < 69.44242631 {
		return 21
	}
	if absLat < 70.45451075 {
		return 20
	}
	if absLat < 71.45986473 {
		return 19
	}
	if absLat < 72.45884545 {
		return 18
	}
	if absLat < 73.45177442 {
		return 17
	}
	if absLat < 74.43893416 {
		return 16
	}
	if absLat < 75.42056257 {
		return 15
	}
	if absLat < 76.39684391 {
		return 14
	}
	if absLat < 77.36789461 {
		return 13
	}
	if absLat < 78.33374083 {
		return 12
	}
	if absLat < 79.29428225 {
		return 11
	}
	if absLat < 80.24923213 {
		return 10
	}
	if absLat < 81.19801349 {
		return 9
	}
	if absLat < 82.13956981 {
		return 8
	}
	if absLat < 83.07199445 {
		return 7
	}
	if absLat < 83.99173563 {
		return 6
	}
	if absLat < 84.89166191 {
		return 5
	}
	if absLat < 85.75541621 {
		return 4
	}
	if absLat < 86.53536998 {
		return 3
	}
	if absLat < 87.00000000 {
		return 2
	}
	return 1
}
