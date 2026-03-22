/**
 * Frontend Unit Tests
 * Tests for key modules: InputValidator, API_CONFIG, CSRFManager
 * 
 * Usage: Open test-runner.html in a browser
 */

// ==================== 测试框架 ====================

const TestRunner = (function() {
    let passed = 0;
    let failed = 0;
    let tests = [];

    /**
     * 添加测试
     * @param {string} name - 测试名称
     * @param {Function} testFn - 测试函数
     */
    function test(name, testFn) {
        tests.push({ name, testFn });
    }

    /**
     * 运行所有测试
     */
    function run() {
        passed = 0;
        failed = 0;
        const results = [];

        tests.forEach(({ name, testFn }) => {
            try {
                testFn();
                passed++;
                results.push({ name, status: 'PASS', error: null });
                console.log(`✓ ${name}`);
            } catch (error) {
                failed++;
                results.push({ name, status: 'FAIL', error: error.message });
                console.error(`✗ ${name}: ${error.message}`);
            }
        });

        console.log(`\nResults: ${passed} passed, ${failed} failed`);
        return { passed, failed, results };
    }

    /**
     * 断言相等
     */
    function assertEqual(actual, expected, message = '') {
        if (actual !== expected) {
            throw new Error(`${message} Expected ${expected}, got ${actual}`);
        }
    }

    /**
     * 断言为真
     */
    function assertTrue(value, message = '') {
        if (!value) {
            throw new Error(`${message} Expected true, got ${value}`);
        }
    }

    /**
     * 断言为假
     */
    function assertFalse(value, message = '') {
        if (value) {
            throw new Error(`${message} Expected false, got ${value}`);
        }
    }

    /**
     * 断言抛出错误
     */
    function assertThrows(fn, message = '') {
        try {
            fn();
            throw new Error(`${message} Expected function to throw`);
        } catch (e) {
            if (e.message.includes('Expected function to throw')) {
                throw e;
            }
            // 预期的错误
        }
    }

    /**
     * 断言类型
     */
    function assertType(value, type, message = '') {
        const actualType = typeof value;
        if (actualType !== type) {
            throw new Error(`${message} Expected type ${type}, got ${actualType}`);
        }
    }

    /**
     * 断言数组包含
     */
    function assertContains(array, item, message = '') {
        if (!array.includes(item)) {
            throw new Error(`${message} Expected array to contain ${item}`);
        }
    }

    /**
     * 断言对象有属性
     */
    function assertHasProperty(obj, prop, message = '') {
        if (!(prop in obj)) {
            throw new Error(`${message} Expected object to have property ${prop}`);
        }
    }

    /**
     * 重置测试
     */
    function reset() {
        tests = [];
        passed = 0;
        failed = 0;
    }

    return {
        test,
        run,
        reset,
        assertEqual,
        assertTrue,
        assertFalse,
        assertThrows,
        assertType,
        assertContains,
        assertHasProperty,
    };
})();

// ==================== InputValidator 测试 ====================

function runInputValidatorTests() {
    TestRunner.reset();
    const { test, assertEqual, assertTrue, assertFalse } = TestRunner;

    test('validatePort - 有效端口', () => {
        assertTrue(InputValidator.validatePort(53));
        assertTrue(InputValidator.validatePort(80));
        assertTrue(InputValidator.validatePort(443));
        assertTrue(InputValidator.validatePort(65535));
    });

    test('validatePort - 无效端口', () => {
        assertFalse(InputValidator.validatePort(0));
        assertFalse(InputValidator.validatePort(-1));
        assertFalse(InputValidator.validatePort(65536));
        assertFalse(InputValidator.validatePort(100000));
    });

    test('validatePort - 字符串输入', () => {
        assertTrue(InputValidator.validatePort('53'));
        assertFalse(InputValidator.validatePort('abc'));
        assertFalse(InputValidator.validatePort(''));
    });

    test('validateTimeout - 有效超时', () => {
        assertTrue(InputValidator.validateTimeout(100));
        assertTrue(InputValidator.validateTimeout(5000));
        assertTrue(InputValidator.validateTimeout(30000));
    });

    test('validateTimeout - 无效超时', () => {
        assertFalse(InputValidator.validateTimeout(0));
        assertFalse(InputValidator.validateTimeout(50));
        assertFalse(InputValidator.validateTimeout(31000));
    });

    test('validateCacheSize - 有效大小', () => {
        assertTrue(InputValidator.validateCacheSize(0));
        assertTrue(InputValidator.validateCacheSize(1000));
        assertTrue(InputValidator.validateCacheSize(10000000));
    });

    test('validateCacheSize - 无效大小', () => {
        assertFalse(InputValidator.validateCacheSize(-1));
        assertFalse(InputValidator.validateCacheSize(10000001));
    });

    test('safeParseInt - 正常值', () => {
        assertEqual(safeParseInt('123', 0), 123);
        assertEqual(safeParseInt(456, 0), 456);
    });

    test('safeParseInt - NaN 处理', () => {
        assertEqual(safeParseInt('abc', 10), 10);
        assertEqual(safeParseInt(undefined, 5), 5);
        assertEqual(safeParseInt(null, 0), 0);
    });

    test('safeParseFloat - 正常值', () => {
        assertEqual(safeParseFloat('3.14', 0), 3.14);
        assertEqual(safeParseFloat(2.5, 0), 2.5);
    });

    test('safeParseFloat - NaN 处理', () => {
        assertEqual(safeParseFloat('abc', 1.0), 1.0);
        assertEqual(safeParseFloat(undefined, 2.5), 2.5);
    });

    return TestRunner.run();
}

// ==================== API_CONFIG 测试 ====================

function runAPIConfigTests() {
    TestRunner.reset();
    const { test, assertEqual, assertTrue, assertHasProperty } = TestRunner;

    test('API_CONFIG - 基础结构', () => {
        assertHasProperty(API_CONFIG, 'baseURL');
        assertHasProperty(API_CONFIG, 'endpoints');
        assertEqual(API_CONFIG.baseURL, '/api');
    });

    test('API_CONFIG - 端点定义', () => {
        assertHasProperty(API_CONFIG.endpoints, 'stats');
        assertHasProperty(API_CONFIG.endpoints, 'config');
        assertHasProperty(API_CONFIG.endpoints, 'csrfToken');
    });

    test('API_CONFIG - getUrl 方法', () => {
        const url = API_CONFIG.getUrl('stats');
        assertEqual(url, '/api/stats');
    });

    test('API_CONFIG - getUrl 带参数', () => {
        const url = API_CONFIG.getUrl('stats', { days: 7 });
        assertEqual(url, '/api/stats?days=7');
    });

    test('API_CONFIG - getStatsUrl 方法', () => {
        const url = API_CONFIG.getStatsUrl('stats', 30);
        assertTrue(url.includes('days=30'));
    });

    test('API_CONFIG - 无效端点', () => {
        const url = API_CONFIG.getUrl('nonexistent');
        assertEqual(url, '');
    });

    return TestRunner.run();
}

// ==================== VirtualList 测试 ====================

function runVirtualListTests() {
    TestRunner.reset();
    const { test, assertEqual, assertTrue, assertFalse, assertThrows } = TestRunner;

    test('VirtualList - 模块存在', () => {
        assertTrue(typeof VirtualList !== 'undefined');
        assertHasProperty(VirtualList, 'create');
        assertHasProperty(VirtualList, 'createPaginated');
    });

    test('VirtualList.create - 需要容器', () => {
        assertThrows(() => {
            VirtualList.create({});
        });
    });

    test('VirtualList.createPaginated - 需要容器', () => {
        assertThrows(() => {
            VirtualList.createPaginated({});
        });
    });

    test('VirtualList.create - 返回正确的方法', () => {
        const container = document.createElement('div');
        const instance = VirtualList.create({
            container,
            renderItem: (item) => {
                const div = document.createElement('div');
                div.textContent = item;
                return div;
            }
        });

        assertHasProperty(instance, 'setData');
        assertHasProperty(instance, 'scrollToIndex');
        assertHasProperty(instance, 'updateConfig');
        assertHasProperty(instance, 'destroy');
        assertHasProperty(instance, 'getState');
    });

    test('VirtualList.createPaginated - 返回正确的方法', () => {
        const container = document.createElement('div');
        const instance = VirtualList.createPaginated({
            container,
            renderItem: (item) => {
                const div = document.createElement('div');
                div.textContent = item;
                return div;
            }
        });

        assertHasProperty(instance, 'setData');
        assertHasProperty(instance, 'goToPage');
        assertHasProperty(instance, 'destroy');
        assertHasProperty(instance, 'getCurrentPage');
        assertHasProperty(instance, 'getTotalPages');
    });

    test('VirtualList - setData 和 getState', () => {
        const container = document.createElement('div');
        const instance = VirtualList.create({
            container,
            threshold: 5,
            renderItem: (item) => {
                const div = document.createElement('div');
                div.textContent = item;
                return div;
            }
        });

        instance.setData([1, 2, 3]);
        const state = instance.getState();
        assertEqual(state.dataLength, 3);
        assertFalse(state.isVirtualEnabled); // 少于阈值
    });

    test('VirtualList - 虚拟滚动启用', () => {
        const container = document.createElement('div');
        const instance = VirtualList.create({
            container,
            threshold: 5,
            renderItem: (item) => {
                const div = document.createElement('div');
                div.textContent = item;
                return div;
            }
        });

        const largeData = Array.from({ length: 100 }, (_, i) => i);
        instance.setData(largeData);
        const state = instance.getState();
        assertEqual(state.dataLength, 100);
        assertTrue(state.isVirtualEnabled); // 超过阈值
    });

    test('VirtualList - destroy 清理', () => {
        const container = document.createElement('div');
        const instance = VirtualList.create({
            container,
            renderItem: (item) => {
                const div = document.createElement('div');
                div.textContent = item;
                return div;
            }
        });

        instance.setData([1, 2, 3]);
        instance.destroy();
        assertEqual(container.innerHTML, '');
    });

    return TestRunner.run();
}

// ==================== CSRFManager 测试 ====================

function runCSRFManagerTests() {
    TestRunner.reset();
    const { test, assertEqual, assertTrue, assertHasProperty } = TestRunner;

    test('CSRFManager - 模块存在', () => {
        assertTrue(typeof CSRFManager !== 'undefined');
        assertHasProperty(CSRFManager, 'getToken');
        assertHasProperty(CSRFManager, 'addCsrfHeader');
        assertHasProperty(CSRFManager, 'secureFetch');
        assertHasProperty(CSRFManager, 'handleCsrfError');
        assertHasProperty(CSRFManager, 'cleanup');
    });

    test('CSRFManager - addCsrfHeader 返回 Promise', () => {
        // 注意：这个测试需要模拟 fetch
        const result = CSRFManager.addCsrfHeader({});
        assertTrue(result instanceof Promise);
    });

    test('CSRFManager - cleanup 不抛出错误', () => {
        CSRFManager.cleanup(); // 应该不抛出错误
        assertTrue(true);
    });

    return TestRunner.run();
}

// ==================== AccessibilityEnhancer 测试 ====================

function runAccessibilityTests() {
    TestRunner.reset();
    const { test, assertEqual, assertTrue, assertHasProperty } = TestRunner;

    test('AccessibilityEnhancer - 模块存在', () => {
        assertTrue(typeof AccessibilityEnhancer !== 'undefined');
        assertHasProperty(AccessibilityEnhancer, 'init');
        assertHasProperty(AccessibilityEnhancer, 'announce');
        assertHasProperty(AccessibilityEnhancer, 'enhanceElement');
        assertHasProperty(AccessibilityEnhancer, 'setLoadingState');
    });

    test('AccessibilityEnhancer - enhanceElement 添加属性', () => {
        const element = document.createElement('div');
        AccessibilityEnhancer.enhanceElement(element, {
            role: 'button',
            label: 'Test Button',
            expanded: false
        });

        assertEqual(element.getAttribute('role'), 'button');
        assertEqual(element.getAttribute('aria-label'), 'Test Button');
        assertEqual(element.getAttribute('aria-expanded'), 'false');
    });

    test('AccessibilityEnhancer - announce 不抛出错误', () => {
        AccessibilityEnhancer.announce('Test message', 'status');
        assertTrue(true);
    });

    return TestRunner.run();
}

// ==================== 运行所有测试 ====================

function runAllTests() {
    console.log('========== Running InputValidator Tests ==========');
    const validatorResults = runInputValidatorTests();

    console.log('\n========== Running API_CONFIG Tests ==========');
    const apiConfigResults = runAPIConfigTests();

    console.log('\n========== Running VirtualList Tests ==========');
    const virtualListResults = runVirtualListTests();

    console.log('\n========== Running CSRFManager Tests ==========');
    const csrfResults = runCSRFManagerTests();

    console.log('\n========== Running Accessibility Tests ==========');
    const a11yResults = runAccessibilityTests();

    // 汇总结果
    const totalPassed = validatorResults.passed + apiConfigResults.passed + 
                        virtualListResults.passed + csrfResults.passed + a11yResults.passed;
    const totalFailed = validatorResults.failed + apiConfigResults.failed + 
                        virtualListResults.failed + csrfResults.failed + a11yResults.failed;

    console.log('\n========== Test Summary ==========');
    console.log(`Total: ${totalPassed} passed, ${totalFailed} failed`);

    return {
        totalPassed,
        totalFailed,
        validatorResults,
        apiConfigResults,
        virtualListResults,
        csrfResults,
        a11yResults
    };
}

// 导出测试函数
if (typeof module !== 'undefined' && module.exports) {
    module.exports = {
        TestRunner,
        runAllTests,
        runInputValidatorTests,
        runAPIConfigTests,
        runVirtualListTests,
        runCSRFManagerTests,
        runAccessibilityTests,
    };
}
