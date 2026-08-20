package gen

// The test list for gen
//
// Hand-checkable inputs beat fixtures here. "the cat" gives letters
// t:2 h:1 e:1 c:1 a:1 and bigrams th he ca at — and the assertion that earns
// its keep is ec is absent, since that's the cross-boundary pair your tokenising
// choice exists to exclude.
//
// Beyond that: boilerplate present/absent, case folding, whatever you decide
// about apostrophes, and determinism across two runs.
