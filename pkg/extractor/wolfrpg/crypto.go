package wolfrpg

const (
	randMultiplier = 0x343FD
	randIncrement  = 0x269EC3
	randMax        = 0x7FFF
	maxInt32       = 0x7FFFFFFF
)

// MSVCRandom é a implementação pura em Go do gerador LCG da MSVCRT (msvcrt.dll).
type MSVCRandom struct {
	seed uint32
}

func NewMSVCRandom(seed uint32) *MSVCRandom {
	return &MSVCRandom{seed: seed & maxInt32}
}

func (r *MSVCRandom) Srand(seed uint32) {
	r.seed = seed & maxInt32
}

func (r *MSVCRandom) Rand(mask uint32) uint32 {
	r.seed = (r.seed*randMultiplier + randIncrement) & maxInt32
	return (r.seed >> 16) & mask
}

// DecryptDatV1 descriptografa arquivos .dat do Wolf RPG v1/v2 utilizando seeds do cabeçalho.
func DecryptDatV1(data []byte, seeds []uint32, intervals []int) []byte {
	out := make([]byte, len(data))
	copy(out, data)

	rng := NewMSVCRandom(0)
	for i, seed := range seeds {
		rng.Srand(seed)
		step := 1
		if i < len(intervals) && intervals[i] > 0 {
			step = intervals[i]
		}
		for j := 0; j < len(out); j += step {
			out[j] ^= byte((rng.Rand(0xFFFF) >> 12) & 0xFF)
		}
	}
	return out
}
