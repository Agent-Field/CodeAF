"""Hidden grading suite for pathmatch. Never shown to the agent.

Twelve groups, deliberately small and independent, spanning an easy-to-hard
range. t1's three failing groups were all-or-nothing cliffs and every arm-A
replicate scored exactly 0.667; the point of twelve fine-grained rules is that a
submission can land anywhere between, so the task measures a gradient instead of
a threshold.

Groups 1–4 are the parts any competent glob implementation gets right. Groups
5–8 need the spec read once. Groups 9–12 are the four places this spec
deliberately diverges from gitignore, and are where a submission that
pattern-matched its way to a familiar answer loses.

  1  literals and case
  2  `?`
  3  `*` stays inside a segment
  4  `**` crosses segments, whole-segment only
  5  character classes and ranges
  6  escaping
  7  anchoring
  8  compile of blanks, comments and errors
  9  `[!…]` negates and `[^…]` does not          (divergence)
 10  directory-only means the path ends in `/`   (divergence)
 11  first matching pattern wins                 (divergence)
 12  select ordering and de-duplication          (divergence)
"""

import pytest

from pathmatch import PatternError, compile_pattern, matches, select


# ---- group 1: literals and case --------------------------------------------

def test_GROUP_1_exact_literal():
    assert matches("README.md", "README.md")
    assert not matches("README.md", "readme.md")


def test_GROUP_1_is_always_case_sensitive():
    assert not matches("SRC/Main.py", "src/main.py")
    assert matches("SRC/Main.py", "SRC/Main.py")


def test_GROUP_1_literal_in_a_subdirectory():
    assert matches("src/main.py", "src/main.py")
    assert not matches("src/main.py", "src/other.py")


# ---- group 2: `?` -----------------------------------------------------------

def test_GROUP_2_question_matches_exactly_one():
    assert matches("a?c", "abc")
    assert not matches("a?c", "ac")
    assert not matches("a?c", "abbc")


def test_GROUP_2_question_never_matches_a_slash():
    assert not matches("a?c", "a/c")


# ---- group 3: `*` stays inside a segment ------------------------------------

def test_GROUP_3_star_matches_zero_or_more():
    assert matches("*.py", "main.py")
    assert matches("a*b", "ab")
    assert matches("a*b", "axxxb")


def test_GROUP_3_star_never_crosses_a_slash():
    assert not matches("src/*.py", "src/pkg/main.py")
    assert matches("src/*.py", "src/main.py")


def test_GROUP_3_a_run_of_stars_inside_a_segment_is_one_star():
    # `**` only crosses segments when it is the whole segment.
    assert not matches("src/a**b/x.py", "src/a/q/b/x.py")
    assert matches("src/a**b/x.py", "src/aQQb/x.py")


# ---- group 4: `**` crosses segments -----------------------------------------

def test_GROUP_4_double_star_spans_many_segments():
    assert matches("src/**/main.py", "src/main.py")
    assert matches("src/**/main.py", "src/a/main.py")
    assert matches("src/**/main.py", "src/a/b/c/main.py")


def test_GROUP_4_double_star_at_the_end_takes_the_rest():
    assert matches("src/**", "src/a/b.py")
    assert matches("src/**", "src/b.py")


def test_GROUP_4_bare_double_star_matches_everything():
    assert matches("**", "a")
    assert matches("**", "a/b/c.py")


def test_GROUP_4_double_star_does_not_escape_its_prefix():
    assert not matches("src/**/main.py", "lib/a/main.py")


# ---- group 5: character classes ---------------------------------------------

def test_GROUP_5_class_members_and_ranges():
    assert matches("[abc].py", "a.py")
    assert matches("[abc].py", "c.py")
    assert not matches("[abc].py", "d.py")
    assert matches("[a-f]1", "c1")
    assert not matches("[a-f]1", "g1")


def test_GROUP_5_class_matches_exactly_one_character():
    assert not matches("[abc].py", "ab.py")


def test_GROUP_5_a_range_spanning_slash_still_cannot_match_one():
    # '.'(46) to '9'(57) spans '/'(47) in ASCII, so a naive regex class would
    # match it. Paths are split on '/' before any class is consulted, so no
    # class can ever see one.
    assert not matches("a[.-9]c", "a/c")
    assert matches("a[.-9]c", "a5c")


# ---- group 6: escaping ------------------------------------------------------

def test_GROUP_6_escaped_wildcards_are_literal():
    assert matches(r"a\*b", "a*b")
    assert not matches(r"a\*b", "axxb")
    assert matches(r"a\?b", "a?b")
    assert not matches(r"a\?b", "axb")


def test_GROUP_6_escaped_bracket_and_backslash():
    assert matches(r"a\[b", "a[b")
    assert matches(r"a\\b", "a\\b")


def test_GROUP_6_escaped_bang_is_not_a_negation():
    assert select([r"\!important.py"], ["!important.py"]) == ["!important.py"]


# ---- group 7: anchoring -----------------------------------------------------

def test_GROUP_7_a_pattern_with_a_slash_is_anchored():
    assert matches("src/main.py", "src/main.py")
    assert not matches("src/main.py", "lib/src/main.py")


def test_GROUP_7_a_pattern_without_a_slash_matches_any_segment():
    assert matches("main.py", "src/pkg/main.py")
    assert matches("build", "a/build/c.txt")


def test_GROUP_7_an_anchored_pattern_must_consume_the_whole_path():
    assert not matches("src/a", "src/a/b.py")
    assert not matches("src/main.py", "src/main.py/extra")
    # the unanchored form of the same name does match, because it is looking
    # at segments rather than at the whole path
    assert matches("src", "src/main.py")


# ---- group 8: compile of blanks, comments and errors ------------------------

def test_GROUP_8_blank_and_comment_compile_to_none():
    assert compile_pattern("") is None
    assert compile_pattern("   ") is None
    assert compile_pattern("# a comment") is None


def test_GROUP_8_escaped_hash_is_a_literal():
    assert compile_pattern(r"\#notacomment") is not None
    assert matches(r"\#notacomment", "#notacomment")


def test_GROUP_8_trailing_spaces_are_stripped_unless_escaped():
    assert matches("a.py   ", "a.py")
    assert matches("a\\ ", "a ")
    assert not matches("a.py   ", "a.py   ")


def test_GROUP_8_broken_patterns_raise():
    with pytest.raises(PatternError):
        compile_pattern("a[bc")
    with pytest.raises(PatternError):
        compile_pattern("a\\")


def test_GROUP_8_comments_are_ignored_by_select():
    assert select(["# nothing", "", "*.py"], ["a.py", "b.txt"]) == ["a.py"]


# ---- group 9: `[!…]` negates, `[^…]` does not  (divergence) -----------------

def test_GROUP_9_bang_negates_a_class():
    assert matches("[!abc].py", "d.py")
    assert not matches("[!abc].py", "a.py")


def test_GROUP_9_caret_is_a_literal_class_member():
    # NOT a negation: `^` is an ordinary member of the class.
    assert matches("[^abc].py", "^.py")
    assert matches("[^abc].py", "a.py")
    assert not matches("[^abc].py", "d.py")


def test_GROUP_9_negated_class_still_refuses_a_slash():
    assert not matches("a[!x]c", "a/c")


# ---- group 10: directory-only  (divergence) ---------------------------------

def test_GROUP_10_trailing_slash_requires_a_directory_path():
    assert matches("build/", "build/")
    assert not matches("build/", "build")


def test_GROUP_10_directory_only_does_not_take_the_contents():
    # The *path* must end in a slash. A file underneath does not.
    assert not matches("build/", "build/out.o")


def test_GROUP_10_directory_only_is_still_unanchored():
    assert matches("build/", "a/build/")


def test_GROUP_10_without_the_slash_a_directory_still_matches():
    assert matches("build", "build/")


# ---- group 11: first matching pattern wins  (divergence) --------------------

def test_GROUP_11_first_match_decides_not_last():
    # gitignore would let the later `!` win; here the first pattern decides.
    assert select(["*.py", "!keep.py"], ["keep.py"]) == ["keep.py"]


def test_GROUP_11_a_negation_placed_first_excludes():
    assert select(["!keep.py", "*.py"], ["keep.py", "other.py"]) == ["other.py"]


def test_GROUP_11_unmatched_paths_are_not_selected():
    assert select(["*.py"], ["a.txt"]) == []


def test_GROUP_11_later_patterns_still_apply_to_other_paths():
    chosen = select(["!secret.py", "*.py"], ["secret.py", "a.py", "b.txt"])
    assert chosen == ["a.py"]


# ---- group 12: select ordering and de-duplication  (divergence) -------------

def test_GROUP_12_output_follows_input_order_not_pattern_order():
    chosen = select(["*.txt", "*.py"], ["z.py", "a.txt", "m.py"])
    assert chosen == ["z.py", "a.txt", "m.py"]


def test_GROUP_12_duplicates_collapse_to_first_appearance():
    assert select(["*.py"], ["a.py", "b.py", "a.py"]) == ["a.py", "b.py"]


def test_GROUP_12_duplicate_of_an_excluded_path_stays_excluded():
    assert select(["!a.py", "*.py"], ["a.py", "a.py", "b.py"]) == ["b.py"]


def test_GROUP_12_empty_inputs():
    assert select([], ["a.py"]) == []
    assert select(["*.py"], []) == []
