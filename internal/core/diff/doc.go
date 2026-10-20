// Package diff implements line-based text differencing, unified diff
// formatting, and line change statistics for TaskSmith.
//
// # Shortest Edit Script (Myers Algorithm)
//
// [MyersDiff] implements Eugene W. Myers' O((N+M)D) greedy shortest-edit-script
// algorithm ("An O(ND) Difference Algorithm and Its Variations", 1986). Given
// two slices of lines A and B, it searches the edit graph along diagonals
// k = x - y in increasing order of edit distance D (the number of non-diagonal
// steps), advancing greedily along "snakes" of matching lines ([OpEqual]) before
// backtracking through the recorded frontier trace to reconstruct the minimal
// sequence of [Edit] operations ([OpEqual], [OpDelete], and [OpInsert]).
//
// # Unified Diff Formatting
//
// [FormatUnified] normalizes CRLF ("\r\n") and LF ("\n") line endings, computes
// the shortest edit script via [MyersDiff], and groups contiguous changes into
// standard unified diff hunks surrounded by up to 3 lines of unchanged context.
// Adjacent changes separated by 6 or fewer unchanged lines are coalesced into a
// single hunk. If both inputs are empty or produce no net edits, [FormatUnified]
// returns an empty string.
//
// # Line Change Statistics
//
// [Stats] computes the line-level addition ([OpInsert]) and deletion ([OpDelete])
// counts between two text payloads using the same line splitting and CRLF
// normalization rules as [FormatUnified].
package diff
