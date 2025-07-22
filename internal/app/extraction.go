package app

import (
	"math"
	"strings"

	"go1090/internal/adsb"
)

// extractCallsign extracts callsign from aircraft identification message (dump1090 style)
func (app *Application) extractCallsign(data []byte) string {
	if len(data) < 11 {
		return ""
	}

	// Debug: print the raw data for analysis
	if app.verbose {
		app.logger.Debugf("Callsign raw data: %x", data[:11])
	}

	// ME (Message Extended) field starts at byte 4 for DF17/18
	me := data[4:]
	if len(me) < 7 {
		return ""
	}

	// Extract callsign using dump1090's exact method: bits 9-14, 15-20, 21-26, etc. (1-based)
	var callsign [9]byte // 8 chars + null terminator

	callsign[0] = adsb.ADSBCharset[app.getBits(me, 9, 14)]  // bits 9-14 in ME
	callsign[1] = adsb.ADSBCharset[app.getBits(me, 15, 20)] // bits 15-20 in ME
	callsign[2] = adsb.ADSBCharset[app.getBits(me, 21, 26)] // bits 21-26 in ME
	callsign[3] = adsb.ADSBCharset[app.getBits(me, 27, 32)] // bits 27-32 in ME
	callsign[4] = adsb.ADSBCharset[app.getBits(me, 33, 38)] // bits 33-38 in ME
	callsign[5] = adsb.ADSBCharset[app.getBits(me, 39, 44)] // bits 39-44 in ME
	callsign[6] = adsb.ADSBCharset[app.getBits(me, 45, 50)] // bits 45-50 in ME
	callsign[7] = adsb.ADSBCharset[app.getBits(me, 51, 56)] // bits 51-56 in ME
	callsign[8] = 0

	// Validate callsign (dump1090 style validation)
	valid := true
	for i := 0; i < 8; i++ {
		if !((callsign[i] >= 'A' && callsign[i] <= 'Z') ||
			(callsign[i] >= '0' && callsign[i] <= '9') ||
			callsign[i] == ' ') {
			valid = false
			break
		}
	}

	if !valid {
		if app.verbose {
			app.logger.Debugf("Invalid callsign characters detected")
		}
		return ""
	}

	result := strings.TrimSpace(string(callsign[:8]))
	if app.verbose {
		app.logger.Debugf("Extracted callsign: '%s'", result)
	}
	return result
}

// getBits extracts bits from data using 1-based indexing (like dump1090)
func (app *Application) getBits(data []byte, firstBit, lastBit int) uint8 {
	if firstBit < 1 || lastBit < firstBit || len(data) == 0 {
		return 0
	}

	// Convert to 0-based indexing
	fbi := firstBit - 1
	lbi := lastBit - 1
	nbi := lastBit - firstBit + 1

	if nbi > 8 {
		return 0 // Can't extract more than 8 bits into uint8
	}

	fby := fbi / 8
	lby := lbi / 8

	if lby >= len(data) {
		return 0
	}

	shift := 7 - (lbi % 8)
	topMask := uint8(0xFF >> (fbi % 8))

	if fby == lby {
		// All bits in the same byte
		return (data[fby] & topMask) >> shift
	} else if lby == fby+1 {
		// Bits span two bytes
		return ((data[fby] & topMask) << (8 - shift)) | (data[lby] >> shift)
	} else if lby == fby+2 {
		// Bits span three bytes (needed for callsign extraction)
		return ((data[fby] & topMask) << (16 - shift)) | (data[fby+1] << (8 - shift)) | (data[lby] >> shift)
	}

	// For even more complex cases (velocity extraction needs up to 10-bit values)
	var result uint32
	for i := fby; i <= lby && i < len(data); i++ {
		if i == fby {
			result = uint32(data[i] & topMask)
		} else {
			result = (result << 8) | uint32(data[i])
		}
	}

	// Handle larger bit extractions for velocity fields
	if nbi <= 32 {
		return uint8((result >> shift) & ((1 << nbi) - 1))
	}

	return uint8(result >> shift)
}

// getBitsUint16 extracts bits from data using 1-based indexing, returning uint16 for larger values
func (app *Application) getBitsUint16(data []byte, firstBit, lastBit int) uint16 {
	if firstBit < 1 || lastBit < firstBit || len(data) == 0 {
		return 0
	}

	// Convert to 0-based indexing
	fbi := firstBit - 1
	lbi := lastBit - 1
	nbi := lastBit - firstBit + 1

	if nbi > 16 {
		return 0 // Can't extract more than 16 bits into uint16
	}

	fby := fbi / 8
	lby := lbi / 8

	if lby >= len(data) {
		return 0
	}

	shift := 7 - (lbi % 8)
	topMask := uint8(0xFF >> (fbi % 8))

	var result uint32
	for i := fby; i <= lby && i < len(data); i++ {
		if i == fby {
			result = uint32(data[i] & topMask)
		} else {
			result = (result << 8) | uint32(data[i])
		}
	}

	return uint16((result >> shift) & ((1 << nbi) - 1))
}

// extractAltitude extracts altitude from surveillance or position messages
func (app *Application) extractAltitude(data []byte) int {
	if len(data) < 6 {
		return 0
	}

	// Extract 13-bit altitude field (different positions for different message types)
	df := (data[0] >> 3) & 0x1F

	var altCode uint16

	if df == 4 || df == 20 {
		// Surveillance altitude reply - bits 20-32
		altCode = (uint16(data[2]&0x1F) << 8) | uint16(data[3])
	} else if df == 17 || df == 18 {
		// Extended squitter - altitude is in ME field bits 9-20 (AC12 field)
		// ME starts at byte 4, so bits 9-20 of ME are in bytes 5-6 of the full message
		// Extract 12-bit AC12 field properly
		altCode = (uint16(data[5]&0x1F) << 7) | (uint16(data[6]) >> 1)
	} else {
		return 0
	}

	if altCode == 0 {
		return 0
	}

	// Decode altitude using dump1090's AC12 method
	// Check Q-bit (bit 4 of the 12-bit field)
	qBit := (altCode & 0x10) != 0

	if qBit {
		// 25-foot resolution encoding (dump1090's decodeAC12Field)
		// N is the 11 bit integer resulting from the removal of bit Q at bit 4
		n := ((altCode & 0x0FE0) >> 1) | (altCode & 0x000F)
		// The final altitude is the resulting number multiplied by 25, minus 1000
		return int(n)*25 - 1000
	} else {
		// 100-foot resolution (Gillham Mode C encoding)
		// Make N a 13 bit Gillham coded altitude by inserting M=0 at bit 6
		n13 := ((altCode & 0x0FC0) << 1) | (altCode & 0x003F)

		if n13 == 0 {
			return 0
		}

		// Simplified conversion - convert to 500ft increments first
		hundreds := int((n13 >> 8) & 0x07)
		fiveHundreds := int((n13 >> 4) & 0x0F)

		// Combine and convert to feet (each unit = 100ft)
		altitude := (fiveHundreds*5 + hundreds) * 100

		// Sanity check - reject unrealistic altitudes
		if altitude < -2000 || altitude > 60000 {
			return 0
		}

		return altitude
	}
}

// extractSquawk extracts squawk code from surveillance messages
func (app *Application) extractSquawk(data []byte) int {
	if len(data) < 4 {
		return 0
	}

	// Extract 13-bit identity field
	identity := (uint16(data[2]&0x1F) << 8) | uint16(data[3])

	// Convert to 4-digit squawk code
	squawk := 0
	squawk += int((identity>>adsb.SquawkA4A2A1Shift)&adsb.SquawkA4A2A1Mask) * adsb.SquawkAMultiplier // A4 A2 A1
	squawk += int((identity>>adsb.SquawkB4B2B1Shift)&adsb.SquawkB4B2B1Mask) * adsb.SquawkBMultiplier // B4 B2 B1
	squawk += int((identity>>adsb.SquawkC4C2C1Shift)&adsb.SquawkC4C2C1Mask) * adsb.SquawkCMultiplier // C4 C2 C1
	squawk += int((identity>>adsb.SquawkD4D2D1Shift)&adsb.SquawkD4D2D1Mask) * adsb.SquawkDMultiplier // D4 D2 D1

	return squawk
}

// extractVelocity extracts velocity information from airborne velocity messages
func (app *Application) extractVelocity(data []byte) (int, float64, int) {
	if len(data) < 11 {
		if app.verbose {
			app.logger.Debugf("Velocity extraction failed: data too short (%d bytes)", len(data))
		}
		return 0, 0, 0
	}

	// Extract velocity subtype
	subtype := (data[4] >> 1) & 0x07

	if app.verbose {
		app.logger.Debugf("Velocity message: subtype=%d, data=%x", subtype, data[:11])
	}

	if subtype < 1 || subtype > 4 {
		if app.verbose {
			app.logger.Debugf("Velocity extraction failed: unsupported subtype %d (only 1-4 supported)", subtype)
		}
		return 0, 0, 0 // Only handle groundspeed and airspeed subtypes (1-4)
	}

	var groundSpeed int
	var track float64

	if subtype == 1 || subtype == 2 {
		// Ground speed subtypes (dump1090 method)
		// ME field starts at data[4], so velocity bits are in ME[1-4]
		me := data[4:]

		// Extract east-west velocity (bits 15-24 of ME)
		ewRaw := app.getBitsUint16(me, 15, 24)
		// Extract north-south velocity (bits 26-35 of ME)
		nsRaw := app.getBitsUint16(me, 26, 35)

		if app.verbose {
			app.logger.Debugf("Ground speed components: ewDir=%d, ewVel=%d, nsDir=%d, nsVel=%d",
				app.getBits(me, 14, 14), ewRaw, app.getBits(me, 25, 25), nsRaw)
		}

		if ewRaw != 0 && nsRaw != 0 {
			// Convert to signed velocities (dump1090 style)
			ewVel := int(ewRaw-1) * (1 << (subtype - 1)) // subtype 1: *1, subtype 2: *4
			if app.getBits(me, 14, 14) != 0 {
				ewVel = -ewVel
			}

			nsVel := int(nsRaw-1) * (1 << (subtype - 1))
			if app.getBits(me, 25, 25) != 0 {
				nsVel = -nsVel
			}

			// Calculate ground speed and track (dump1090 method)
			groundSpeed = int(math.Sqrt(float64(nsVel*nsVel+ewVel*ewVel)) + 0.5)

			if groundSpeed > 0 {
				track = math.Atan2(float64(ewVel), float64(nsVel)) * 180.0 / math.Pi
				if track < 0 {
					track += 360
				}

				if app.verbose {
					app.logger.Debugf("Valid ground speed: %d kt, track: %.1f°", groundSpeed, track)
				}
			}
		}

	} else if subtype == 3 || subtype == 4 {
		// Airspeed subtypes (dump1090 method)
		me := data[4:]

		// Extract heading (bits 15-24 of ME)
		if app.getBits(me, 14, 14) != 0 {
			track = float64(app.getBitsUint16(me, 15, 24)) * 360.0 / 1024.0
		}

		// Extract airspeed (bits 26-35 of ME)
		airspeedRaw := app.getBitsUint16(me, 26, 35)
		if airspeedRaw != 0 {
			airspeed := int(airspeedRaw-1) * (1 << (subtype - 3)) // subtype 3: *1, subtype 4: *4

			// For airspeed messages, we don't get ground speed directly
			// But we can use airspeed as an approximation
			groundSpeed = airspeed

			if app.verbose {
				app.logger.Debugf("Airspeed data: airspeed=%d, heading=%.1f", airspeed, track)
				if groundSpeed > 0 {
					app.logger.Debugf("Using airspeed as ground speed: %d kt", groundSpeed)
				}
			}
		}
	}

	// Extract vertical rate (common for all subtypes) - dump1090 method
	me := data[4:]
	vrRaw := app.getBitsUint16(me, 38, 46) // bits 38-46 of ME

	var verticalRate int
	if vrRaw != 0 {
		verticalRate = int(vrRaw-1) * 64
		if app.getBits(me, 37, 37) != 0 { // sign bit 37
			verticalRate = -verticalRate
		}
	}

	if app.verbose {
		app.logger.Debugf("Velocity result: groundSpeed=%d, track=%.1f, verticalRate=%d", groundSpeed, track, verticalRate)
		if groundSpeed == 0 && track == 0 && verticalRate == 0 {
			app.logger.Debugf("All velocity values are zero - check message parsing")
		}
	}

	// Return data even if only partial information is available
	// For MSG,4 to be useful, we need at least speed, track, or vertical rate
	if groundSpeed > 0 || track > 0 || verticalRate != 0 {
		return groundSpeed, track, verticalRate
	}

	// Return partial data even if all values are zero, to help with debugging
	return groundSpeed, track, verticalRate
}

// extractPosition extracts latitude and longitude from position messages
func (app *Application) extractPosition(data []byte) (float64, float64) {
	if len(data) < 11 {
		return 0, 0
	}

	icao := app.extractICAO(data)

	// Extract F flag (odd/even)
	fFlag := (data[6] >> 2) & 0x01

	// Extract CPR latitude (17 bits)
	cprLatRaw := ((uint32(data[6]&0x03) << 15) | (uint32(data[7]) << 7) | (uint32(data[8]) >> 1)) & 0x1FFFF

	// Extract CPR longitude (17 bits)
	cprLonRaw := ((uint32(data[8]&0x01) << 16) | (uint32(data[9]) << 8) | uint32(data[10])) & 0x1FFFF

	if app.verbose {
		app.logger.Debugf("CPR position data: ICAO=%06X, F=%d, lat_cpr=%d (%.6f), lon_cpr=%d (%.6f)",
			icao, fFlag, cprLatRaw, float64(cprLatRaw)/adsb.CPR_LAT_MAX, cprLonRaw, float64(cprLonRaw)/adsb.CPR_LON_MAX)
	}

	// Use CPR decoder to get actual coordinates
	return app.cprDecoder.DecodeCPRPosition(icao, uint8(fFlag), cprLatRaw, cprLonRaw)
}

// extractICAO extracts the ICAO address from the message
func (app *Application) extractICAO(data []byte) uint32 {
	if len(data) < 4 {
		return 0
	}
	return (uint32(data[1]) << 16) | (uint32(data[2]) << 8) | uint32(data[3])
}

// extractGroundState extracts ground/airborne state with improved accuracy
func (app *Application) extractGroundState(data []byte) string {
	if len(data) < 5 {
		return "0" // Default to airborne
	}

	df := (data[0] >> 3) & 0x1F

	// For surveillance messages, check VS and FS bits
	if df == 4 || df == 5 || df == 20 || df == 21 {
		// VS (Vertical Status) bit - bit 6
		vs := (data[0] >> 2) & 0x01
		if vs == 1 {
			return "1" // On ground
		}

		// Also check FS (Flight Status)
		fs := (data[0] >> 3) & 0x07
		if fs == 1 || fs == 3 {
			return "1" // On ground
		}
	}

	// For extended squitter messages
	if df == 17 || df == 18 {
		typeCode := (data[4] >> 3) & 0x1F

		// Surface position messages (type codes 5-8)
		if typeCode >= 5 && typeCode <= 8 {
			return "1" // On ground
		}

		// Check CA (Capability) field for DF17
		if df == 17 {
			ca := data[0] & 0x07
			if ca == 4 {
				return "1" // Ground vehicle
			} else if ca == 5 {
				return "0" // Airborne
			}
		}
	}

	return "0" // Default to airborne
}
