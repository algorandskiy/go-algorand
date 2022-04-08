// You should have received a copy of the GNU Affero General Public License
// along with go-algorand.  If not, see <https://www.gnu.org/licenses/>.

//go:build wasm
// +build wasm

package sortition

import "github.com/algorand/go-algorand/crypto"

func Select(money uint64, totalMoney uint64, expectedSize float64, vrfOutput crypto.Digest) uint64 {
	return 0
}
