package styles

import (
	"github.com/masterkeysrd/kite/extras/kitex"
	"github.com/masterkeysrd/tasksmith/internal/ui/theme"
)

func Use[T any](build func(*theme.Scheme) T) T {
	scheme := theme.UseTheme()
	return kitex.UseMemo(func() T { return build(scheme) }, []any{scheme})
}
